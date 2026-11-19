"""Native Python API entry point; no launcher script is required."""

import argparse

import uvicorn


def main() -> None:
    parser = argparse.ArgumentParser(description="Run the MarketMind platform API")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--reload", action="store_true", help="Development hot reload")
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error("port must be between 1 and 65535")
    uvicorn.run(
        "src.presentation.http.main:app", host=args.host, port=args.port, reload=args.reload
    )


if __name__ == "__main__":
    main()
