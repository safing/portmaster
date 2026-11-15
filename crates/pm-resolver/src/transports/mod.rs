pub mod https;
pub mod plain;
pub mod tcp;

pub use https::HttpsResolver;
pub use plain::PlainResolver;
pub use tcp::TcpResolver;
