# VHDL Simulator Backend

Go backend for authentication, design management, and VHDL simulation.

## Run Locally

```bash
go run cmd/server/main.go
```

Default server URL: `http://localhost:8080`

## Environment Variables

Copy values into a local `.env` (do not commit `.env`):

```env
DATABASE_URL=postgres://postgres:your_password@localhost:5432/vhdl_platform?sslmode=disable
PORT=8080
JWT_SECRET=replace_with_a_secure_secret
CORS_ALLOWED_ORIGINS=http://localhost:5173
```

### Variable Notes

- `DATABASE_URL`: PostgreSQL connection string.
- `PORT`: HTTP port for the backend server.
- `JWT_SECRET`: Secret used to sign JWT tokens. Use a long random value in production.
- `CORS_ALLOWED_ORIGINS`: Comma-separated frontend origins allowed to call the API.

Example with multiple origins:

```env
CORS_ALLOWED_ORIGINS=https://your-frontend.vercel.app,http://localhost:5173
```

## Production Checklist

- Set a strong `JWT_SECRET` in deployment environment variables.
- Set `CORS_ALLOWED_ORIGINS` to only trusted frontend domains.
- Set a production `DATABASE_URL`.
- Keep `.env` out of Git and use host-managed secrets.

## API Base Path

All API routes are under:

- `/api`

Health check:

- `GET /health`
