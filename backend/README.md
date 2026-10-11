# Go Backend

The backend is a modular monolith.

Target domain module shape:

    domain/
    application/
    ports/
    infrastructure/
    transport/

Dependency direction:

    transport -> application -> domain -> ports
                                  ^
                           infrastructure

The first implementation milestone is the platform foundation under `backend/internal/platform/`.
