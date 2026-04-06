import torch
import torch.nn as nn
import torch.nn.functional as F

class TabularAutoencoder(nn.Module):
    def __init__(self, input_dim=4): # e.g. bytesSent, bytesReceived, duration
        super(TabularAutoencoder, self).__init__()
        # Encoder
        self.encoder = nn.Sequential(
            nn.Linear(input_dim, 8),
            nn.ReLU(True),
            nn.Linear(8, 4),
            nn.ReLU(True),
            nn.Linear(4, 2)
        )
        # Decoder
        self.decoder = nn.Sequential(
            nn.Linear(2, 4),
            nn.ReLU(True),
            nn.Linear(4, 8),
            nn.ReLU(True),
            nn.Linear(8, input_dim)
        )

    def forward(self, x):
        encoded = self.encoder(x)
        decoded = self.decoder(encoded)
        return decoded
