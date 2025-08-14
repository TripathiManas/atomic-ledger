// main.go

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"time"

	// This is the PostgreSQL driver for Go's database/sql package.
	// The blank identifier _ is used because we only need the driver's side effects (its registration).
	_ "github.com/lib/pq"
)

// db is a global variable to hold the database connection pool.
var db *sql.DB

// Account represents the structure of our 'accounts' table.
// The `json:"..."` tags are used to control how the struct is encoded into JSON.
type Account struct {
	ID      int     `json:"id"`
	Balance float64 `json:"balance"`
}

// Transaction represents the structure of our 'transactions' table.
type Transaction struct {
	ID        int       `json:"id"`
	FromID    int       `json:"from_id"`
	ToID      int       `json:"to_id"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

// TransferRequest is used to decode the JSON body of a transfer request.
type TransferRequest struct {
	FromID int     `json:"from_id"`
	ToID   int     `json:"to_id"`
	Amount float64 `json:"amount"`
}

func main() {
	var err error
	// The connection string points to our CockroachDB cluster.
	// We connect to roach-1, but the driver is smart enough to handle cluster operations.
	// New line
	connStr := "postgresql://root@127.0.0.1:26257/atomic_ledger?sslmode=disable"
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("FATAL: failed to connect to database", err)
	}
	defer db.Close() // Ensure the database connection is closed when main exits.

	// ... after defer db.Close()

log.Println("Attempting to ping the database...") // <-- ADD THIS LINE

// Ping the database to verify the connection is alive.
if err = db.Ping(); err != nil {
    log.Fatal("FATAL: could not ping database", err)
}

log.Println("Successfully connected to CockroachDB cluster.")

// ... rest of the code

	// Ping the database to verify the connection is alive.
	if err = db.Ping(); err != nil {
		log.Fatal("FATAL: could not ping database", err)
	}
	log.Println("Successfully connected to CockroachDB cluster.")

	// --- API Route Handlers ---
	// We define which function handles which API endpoint.
	http.HandleFunc("/api/accounts", accountsHandler)
	http.HandleFunc("/api/transactions", transactionsHandler)
	http.HandleFunc("/api/transfers", transferHandler)
	http.HandleFunc("/api/chaos", chaosHandler)

	// Start the HTTP server on port 8081.
	log.Println("Starting AtomicLedger API server on http://localhost:8081")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatal("FATAL: ListenAndServe failed", err)
	}
}

// accountsHandler handles requests for creating and fetching accounts.
func accountsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		// Fetch all accounts
		rows, err := db.Query("SELECT id, balance FROM accounts ORDER BY id")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		accounts := []Account{}
		for rows.Next() {
			var acc Account
			if err := rows.Scan(&acc.ID, &acc.Balance); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			accounts = append(accounts, acc)
		}
		json.NewEncoder(w).Encode(accounts)

	} else if r.Method == http.MethodPost {
		// Create a new account with a default balance of 1000.
		var newAccount Account
		err := db.QueryRow(
			"INSERT INTO accounts (balance) VALUES (1000) RETURNING id, balance",
		).Scan(&newAccount.ID, &newAccount.Balance)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(newAccount)
	}
}

// transactionsHandler fetches the list of all past transactions.
func transactionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows, err := db.Query("SELECT id, from_id, to_id, amount, created_at FROM transactions ORDER BY created_at DESC LIMIT 20")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	transactions := []Transaction{}
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.FromID, &t.ToID, &t.Amount, &t.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		transactions = append(transactions, t)
	}
	json.NewEncoder(w).Encode(transactions)
}

// transferHandler is the most critical function. It performs the fund transfer inside a database transaction.
func transferHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// --- BEGIN DATABASE TRANSACTION ---
	// A transaction ensures that all commands within it either succeed together or fail together (atomicity).
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
		return
	}
	// Defer a rollback. If the transaction succeeds, tx.Commit() is called first, and this becomes a no-op.
	// If any step fails, the function will exit, and this rollback will execute, undoing any changes.
	defer tx.Rollback()

	// Step 1: Check the sender's balance for sufficient funds.
	var senderBalance float64
	err = tx.QueryRow("SELECT balance FROM accounts WHERE id = $1", req.FromID).Scan(&senderBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, `{"error": "Sender account not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if senderBalance < req.Amount {
		http.Error(w, `{"error": "Insufficient funds"}`, http.StatusBadRequest)
		return
	}

	// Step 2: Debit the amount from the sender's account.
	_, err = tx.Exec("UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromID)
	if err != nil {
		http.Error(w, "Failed to debit sender", http.StatusInternalServerError)
		return
	}

	// Step 3: Credit the amount to the receiver's account.
	_, err = tx.Exec("UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToID)
	if err != nil {
		http.Error(w, "Failed to credit receiver", http.StatusInternalServerError)
		return
	}

	// Step 4: Record the transaction in the transactions log.
	_, err = tx.Exec("INSERT INTO transactions (from_id, to_id, amount) VALUES ($1, $2, $3)", req.FromID, req.ToID, req.Amount)
	if err != nil {
		http.Error(w, "Failed to log transaction", http.StatusInternalServerError)
		return
	}

	// --- COMMIT TRANSACTION ---
	// If all steps have succeeded without error, commit the transaction to make the changes permanent.
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Transfer successful!"})
}

// chaosHandler simulates a node failure by stopping one of the Docker containers.
func chaosHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// The container name is usually <projectname>-<service>-<number>.
	// You can verify this by running `docker ps`.
	containerName := "atomic-ledger-roach-3-1"
	log.Printf("Executing chaos: stopping container %s", containerName)

	cmd := exec.Command("docker", "stop", containerName)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to execute docker stop: %v", err)
		http.Error(w, "Failed to stop node", http.StatusInternalServerError)
		return
	}

	log.Printf("Chaos executed successfully: container %s stopped.", containerName)
	json.NewEncoder(w).Encode(map[string]string{"message": "Successfully stopped node roach-3. The cluster should remain available."})
}