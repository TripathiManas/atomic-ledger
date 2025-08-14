# 🏦 AtomicLedger: A Fault-Tolerant Financial Transaction System

**AtomicLedger** is a full-stack web application that simulates a minimalist banking ledger, allowing for the creation of accounts and the atomic transfer of funds between them. This project was specifically designed to demonstrate the core strengths of a distributed SQL database, focusing on data consistency, high availability, and resilience.

The application's key feature is a "chaos test" that deliberately stops a database node mid-transaction to prove that the system remains operational and, most importantly, that no funds are ever lost or corrupted.

## ## Key Features

* **ACID-Compliant Transactions:** All fund transfers are executed within strict, atomic SQL transactions, guaranteeing that every transaction either completes fully or not at all, with no possibility of data inconsistency.
* **High Availability & Fault Tolerance:** Built on a 3-node CockroachDB cluster, the application can withstand the failure of a database node without any downtime or data loss.
* **Real-Time UI:** A simple and clean React frontend that displays account balances and transaction history, updating in near real-time.
* **Integrated Chaos Test:** A "Simulate Node Failure" button on the frontend triggers a script to stop a random database container, providing a live demonstration of the system's resilience.
* **Containerized Deployment:** The entire application stack (Go backend, CockroachDB cluster) is managed by Docker Compose, allowing for a reproducible, one-command setup.

## ## Tech Stack & Architecture

The project follows a classic three-tier architecture, ensuring a clean separation of concerns.

| Tier          | Technology                               | Role                                                                                                                                                             |
| :------------ | :--------------------------------------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Frontend** | **React.js** | Renders the user interface in the browser. Manages UI state and sends user requests to the backend API.                                                          |
| **Backend** | **Go (Golang)** | Serves the frontend and exposes a RESTful API for all application logic. Manages database connections and enforces business rules within atomic transactions.      |
| **Database** | **CockroachDB** | A 3-node distributed SQL cluster that provides data storage with high availability and strong consistency guarantees.                                            |
| **Orchestration** | **Docker & Docker Compose** | Defines, builds, and runs the entire multi-container application stack, managing the network and dependencies between the backend and the database cluster. |

## ## How to Run Locally

**Prerequisites:**

* Docker Desktop installed and running.
* Go (v1.25+) installed.

1.  **Clone the repository:**
    ```bash
    git clone [https://github.com/TripathiManas/atomic-ledger.git](https://github.com/TripathiManas/atomic-ledger.git)
    cd atomic-ledger
    ```

2.  **Build and run the application:**
    This single command will build the Go application image, start the 3-node CockroachDB cluster, and launch the API server.
    ```bash
    docker compose up --build
    ```

3.  **Access the application:**
    Open your web browser and navigate to:
    **`http://localhost:8081`**

The application is now running.

## ## Key Technical Learnings & Interview Talking Points

This project was a deep dive into the challenges of building reliable, distributed systems. Here are the key takeaways:

#### 1. The 64-bit Integer Problem (A Story of Data Integrity)

* **Problem:** CockroachDB uses 64-bit integers for its `SERIAL` primary keys to guarantee uniqueness in a distributed environment. However, JavaScript's standard `Number` type can only safely represent integers up to `Number.MAX_SAFE_INTEGER` (a 53-bit value). When the frontend received the large 64-bit ID from the backend, it was losing precision and corrupting the value.
* **Investigation:** This led to "Sender account not found" errors, as the mangled ID sent back to the backend did not exist in the database.
* **Solution:** The correct architectural solution was to treat these large, unique identifiers as **strings** throughout the entire application stack. This involved updating the Go structs, the database queries, and the React frontend to handle the IDs as opaque strings, ensuring no data was ever lost or corrupted in transit. This demonstrates a deep understanding of data types and their limitations across different programming languages.

#### 2. The Importance of Atomic Transactions

The core of the application's reliability lies in the `transferHandler` function in `main.go`. By wrapping the entire multi-step process (checking balance, debiting sender, crediting receiver, logging the transaction) within a `db.BeginTx()` and `tx.Commit()` block, we leverage CockroachDB's ACID guarantees. The `defer tx.Rollback()` statement acts as a critical safety net, ensuring that if any step fails, the entire operation is instantly undone, leaving the database in a consistent state.

#### 3. Container Orchestration and Startup Logic

Getting a multi-container application to start reliably is a common challenge. The final `docker-compose.yml` uses a robust strategy:
* The `api` service has a `depends_on` clause with `condition: service_healthy`, which forces it to wait until the `roach-1` container's `healthcheck` passes.
* The `roach-init` service runs a setup script that includes `|| true` after the `cockroach init` command. This makes the script idempotent, meaning it succeeds on the first run but doesn't fail on subsequent runs if the cluster is already initialized.
* The `api` container itself has a `sleep 20` command in its entrypoint, providing a simple but effective delay to ensure the database cluster is fully ready to accept connections.