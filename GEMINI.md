# Wega Catalog API

**Project Overview**

The Wega Catalog API is a Go-based microservice designed to provide automotive filter catalog data (Oil, Air, etc.) from Wega Motors. It is specifically engineered to integrate with N8N and Groq (LLM) to facilitate automated quoting systems via WhatsApp.

**Key Features:**
*   **Filter Search:** Find compatible filters based on vehicle make, model, year, and engine.
*   **Cross-Reference:** Convert competitor part numbers to Wega equivalents.
*   **Stateless Architecture:** Designed for containerized deployment (Docker/Coolify).
*   **LLM Ready:** Structured JSON responses optimized for consumption by Large Language Models.

**Tech Stack**

*   **Language:** Go 1.22
*   **Web Framework:** `github.com/go-chi/chi/v5` (Lightweight router)
*   **Database:** PostgreSQL 17
*   **Driver:** `github.com/jackc/pgx/v5` (High-performance PostgreSQL driver)
*   **Infrastructure:** Docker, Docker Compose, Coolify, Traefik

**Architecture**

The project follows a standard Go project layout with a clean separation of concerns:

*   `cmd/server/`: Entry point (main.go). Initializes config, database, and starts the HTTP server.
*   `internal/config/`: Configuration management (environment variables).
*   `internal/database/`: Database connection pool setup (pgx).
*   `internal/handler/`: HTTP handlers (Controllers). Parses requests and formats responses.
*   `internal/service/`: Business logic. Orchestrates repositories to fulfill user requests (e.g., complex search logic).
*   `internal/repository/`: Data access layer. Executes SQL queries.
*   `internal/model/`: Domain models and DTOs.
*   `docs/`: Documentation (API specifications).

**Building and Running**

**Prerequisites:**
*   Go 1.22+
*   PostgreSQL 17 (accessible via connection string)

**Local Development:**

1.  **Clone and Config:**
    ```bash
    cp .env.example .env
    # Edit .env with your database credentials
    ```

2.  **Run:**
    ```bash
    go run ./cmd/server
    ```

3.  **Docker:**
    ```bash
    docker-compose up -d --build
    ```

**Configuration (`.env`):**
*   `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`: Database connection details.
*   `API_PORT`: Port for the HTTP server (default: 8080).
*   `LOG_LEVEL`: Logging verbosity (default: info).

**API Usage**

The API exposes a RESTful interface. Key endpoints include:

*   **Health Check:** `GET /health`
*   **Search Filters:** `POST /api/v1/filtros/buscar`
    *   Payload: `{"marca": "VW", "modelo": "Gol", "ano": "2020", "motor": "1.0"}`
*   **Cross Reference:** `GET /api/v1/referencia-cruzada?codigo=PH5949`

*Refer to `docs/API.md` for detailed endpoint documentation.*

**Development Conventions**

*   **Logging:** Uses `log/slog` for structured JSON logging.
*   **Routing:** Uses `chi` for routing and middleware (RequestID, RealIP, Logger, Recoverer, Timeout, CORS).
*   **Database:** Direct SQL queries via `pgx` (no ORM observed, ensuring performance).
*   **Error Handling:** Handlers return appropriate HTTP status codes and JSON error messages.
*   **Context:** `context.Context` is passed down from handlers to repositories for timeout and cancellation management.
