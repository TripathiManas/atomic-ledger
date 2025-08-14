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

	_ "github.com/lib/pq"
)

var db *sql.DB

// --- FINAL CORRECTED Data Structures ---
type Account struct {
	ID      string  `json:"id"` // ID is now a string
	Balance float64 `json:"balance"`
}

type Transaction struct {
	ID        string    `json:"id"` // ID is now a string
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

type TransferRequest struct {
	FromID string  `json:"from_id"` // ID is now a string
	ToID   string  `json:"to_id"`
	Amount float64 `json:"amount"`
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func main() {
	var err error
	connStr := "postgresql://root@roach-1:26257/atomic_ledger?sslmode=disable"
	db, err = sql.Open("postgres", connStr)
	if err != nil { log.Fatal("FATAL: failed to connect to database", err) }
	defer db.Close()
	if err = db.Ping(); err != nil { log.Fatal("FATAL: could not ping database", err) }
	log.Println("Successfully connected to CockroachDB cluster.")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts", accountsHandler)
	mux.HandleFunc("/api/transactions", transactionsHandler)
	mux.HandleFunc("/api/transfers", transferHandler)
	mux.HandleFunc("/api/chaos", chaosHandler)
	mux.HandleFunc("/", serveFrontend)

	handler := corsMiddleware(mux)

	log.Println("Starting AtomicLedger server on http://localhost:8081")
	if err := http.ListenAndServe(":8081", handler); err != nil {
		log.Fatal("FATAL: ListenAndServe failed", err)
	}
}

func serveFrontend(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" { w.WriteHeader(http.StatusOK); return }
		next.ServeHTTP(w, r)
	})
}

func accountsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		rows, err := db.Query("SELECT id, balance FROM accounts ORDER BY id")
		if err != nil { respondWithError(w, http.StatusInternalServerError, err.Error()); return }
		defer rows.Close()
		var accounts []Account
		for rows.Next() {
			var acc Account
			if err := rows.Scan(&acc.ID, &acc.Balance); err != nil { respondWithError(w, http.StatusInternalServerError, err.Error()); return }
			accounts = append(accounts, acc)
		}
		json.NewEncoder(w).Encode(accounts)
	} else if r.Method == http.MethodPost {
		var newAccount Account
		err := db.QueryRow("INSERT INTO accounts (balance) VALUES (1000) RETURNING id, balance").Scan(&newAccount.ID, &newAccount.Balance)
		if err != nil { respondWithError(w, http.StatusInternalServerError, err.Error()); return }
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(newAccount)
	}
}
func transactionsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows, err := db.Query("SELECT id, from_id, to_id, amount, created_at FROM transactions ORDER BY created_at DESC LIMIT 20")
	if err != nil { respondWithError(w, http.StatusInternalServerError, err.Error()); return }
	defer rows.Close()
	var transactions []Transaction
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.FromID, &t.ToID, &t.Amount, &t.CreatedAt); err != nil { respondWithError(w, http.StatusInternalServerError, err.Error()); return }
		transactions = append(transactions, t)
	}
	json.NewEncoder(w).Encode(transactions)
}
func transferHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { respondWithError(w, http.StatusBadRequest, "Invalid request body"); return }

    log.Printf("Received transfer request: FromID=%s, ToID=%s, Amount=%.2f", req.FromID, req.ToID, req.Amount)

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to begin transaction"); return }
	defer tx.Rollback()
	var senderBalance float64
	// The database can handle comparing a string ID to the integer column
	err = tx.QueryRow("SELECT balance FROM accounts WHERE id = $1", req.FromID).Scan(&senderBalance)
	if err != nil {
		if err == sql.ErrNoRows { respondWithError(w, http.StatusNotFound, "Sender account not found"); return }
		respondWithError(w, http.StatusInternalServerError, err.Error()); return
	}
	if senderBalance < req.Amount { respondWithError(w, http.StatusBadRequest, "Insufficient funds"); return }
	_, err = tx.Exec("UPDATE accounts SET balance = balance - $1 WHERE id = $2", req.Amount, req.FromID)
	if err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to debit sender"); return }
	_, err = tx.Exec("UPDATE accounts SET balance = balance + $1 WHERE id = $2", req.Amount, req.ToID)
	if err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to credit receiver"); return }
	_, err = tx.Exec("INSERT INTO transactions (from_id, to_id, amount) VALUES ($1, $2, $3)", req.FromID, req.ToID, req.Amount)
	if err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to log transaction"); return }
	if err := tx.Commit(); err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to commit transaction"); return }
	json.NewEncoder(w).Encode(map[string]string{"message": "Transfer successful!"})
}
func chaosHandler(w http.ResponseWriter, r *http.Request) {
	containerName := "atomic-ledger-roach-3-1"
	cmd := exec.Command("docker", "stop", containerName)
	if err := cmd.Run(); err != nil { respondWithError(w, http.StatusInternalServerError, "Failed to stop node"); return }
	json.NewEncoder(w).Encode(map[string]string{"message": "Successfully stopped node roach-3."})
}