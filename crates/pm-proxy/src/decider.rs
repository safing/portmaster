use std::net::{IpAddr, SocketAddr};
use std::sync::Arc;
use crate::binding::LocalBinding;

/// Decision represents the outcome of a routing check for an incoming connection.
#[derive(Debug, Clone)]
pub struct Decision {
    pub remote_ip: IpAddr,
    pub remote_port: u16,
    pub binding: Option<LocalBinding>,
}

impl Decision {
    pub fn new(remote_ip: IpAddr, remote_port: u16) -> Self {
        Self {
            remote_ip,
            remote_port,
            binding: None,
        }
    }

    pub fn with_binding(mut self, binding: LocalBinding) -> Self {
        self.binding = Some(binding);
        self
    }
}

/// Decider is consulted for every new session to determine the upstream destination and local binding.
pub trait Decider: Send + Sync {
    fn decide(&self, local: SocketAddr, peer: SocketAddr) -> Result<Decision, String>;
}

impl<F> Decider for F
where
    F: Fn(SocketAddr, SocketAddr) -> Result<Decision, String> + Send + Sync,
{
    fn decide(&self, local: SocketAddr, peer: SocketAddr) -> Result<Decision, String> {
        (self)(local, peer)
    }
}

pub type SharedDecider = Arc<dyn Decider>;
