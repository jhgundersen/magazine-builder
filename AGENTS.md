# Project Notes

- When restarting the local development server, free port 8080 first and then start the app on `:8080`. Do not keep moving to new ports for stale server processes.
- Useful restart pattern:
  - find the process using `:8080`
  - stop that process
  - run `make run ADDR=:8080`
