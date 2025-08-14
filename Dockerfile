# Use the official Go image as a base
FROM golang:1.25-alpine

# NEW: Install the Docker command-line tool
RUN apk add --no-cache docker-cli

# Set the working directory inside the container
WORKDIR /app

# Copy and download dependencies
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# Copy all source code
COPY *.go ./
COPY index.html ./

# Build the Go application
RUN go build -o /atomic-ledger-server

# Expose the port
EXPOSE 8081

# NEW: The command now waits 20 seconds before starting the server.
# This gives the database cluster ample time to initialize.
CMD ["/bin/sh", "-c", "echo 'API container started, waiting 20s for DB to be ready...' && sleep 20 && /atomic-ledger-server"]