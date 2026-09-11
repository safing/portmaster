import argparse
import logging
from inference import InferenceEngine

def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s")

    parser = argparse.ArgumentParser(description="Portmaster HIDS Sidecar Daemon")
    parser.add_argument("--api-port", type=int, required=True, help="Portmaster Core API Port")
    parser.add_argument("--test-mode", action="store_true", help="Run model instantiation test and exit")

    args = parser.parse_args()

    engine = InferenceEngine(api_port=args.api_port, test_mode=args.test_mode)
    engine.run()

if __name__ == "__main__":
    main()
