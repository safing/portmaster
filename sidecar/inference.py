import os
import sys
import json
import socket
import logging
import requests
import torch
import torch.nn as nn
from model import TabularAutoencoder

# Dummy threshold for anomaly score
ANOMALY_THRESHOLD = 0.5
UDS_SOCKET_PATH = "/tmp/portmaster_telemetry.sock"

class InferenceEngine:
    def __init__(self, api_port: int, test_mode: bool = False):
        self.api_port = api_port
        self.test_mode = test_mode
        self.model = TabularAutoencoder(input_dim=3) # bytesSent, bytesReceived, duration
        self.model.eval()
        self.criterion = nn.MSELoss()

        if os.path.exists(UDS_SOCKET_PATH):
            os.remove(UDS_SOCKET_PATH)

        if not self.test_mode:
            self.server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            self.server.bind(UDS_SOCKET_PATH)
            self.server.listen(1)
            logging.info(f"Listening on {UDS_SOCKET_PATH}")

    def run(self):
        if self.test_mode:
            logging.info("Running in test mode. Exiting.")
            return

        while True:
            conn, _ = self.server.accept()
            try:
                buffer = ""
                while True:
                    data = conn.recv(1024)
                    if not data:
                        break
                    buffer += data.decode('utf-8')
                    if '\n' in buffer:
                        parts = buffer.split('\n')
                        for line in parts[:-1]:
                            if line.strip():
                                self.process_telemetry(line)
                        buffer = parts[-1]
            except Exception as e:
                logging.error(f"Error handling connection: {e}")
            finally:
                conn.close()

    def process_telemetry(self, raw_json: str):
        try:
            data = json.loads(raw_json)
            # Duration safely, max 1 so no div by 0 for now. This is a naive processing
            duration = max(data.get("ended", 0) - data.get("started", 0), 1)
            bytes_sent = float(data.get("bytesSent", 0))
            bytes_recv = float(data.get("bytesReceived", 0))

            # Naive Normalization for dummy model
            x = torch.tensor([bytes_sent/1000.0, bytes_recv/1000.0, duration/60.0], dtype=torch.float32)

            with torch.no_grad():
                reconstructed = self.model(x)
                loss = self.criterion(reconstructed, x).item()

            if loss > ANOMALY_THRESHOLD:
                self.trigger_alert(data, loss)

        except json.JSONDecodeError:
            logging.error(f"Failed to decode JSON: {raw_json}")
        except Exception as e:
            logging.error(f"Error processing telemetry: {e}")

    def trigger_alert(self, data: dict, score: float):
        payload = {
            "pid": data.get("pid"),
            "binaryPath": data.get("binaryPath"),
            "destIP": data.get("destIP"),
            "score": score
        }
        url = f"http://127.0.0.1:{self.api_port}/api/v1/hids/alert"
        try:
            requests.post(url, json=payload, timeout=2)
            logging.warning(f"ALERT TRIGGERED for PID {payload['pid']} (Score {score})")
        except requests.exceptions.RequestException as e:
            logging.error(f"Failed to send alert to Core: {e}")
