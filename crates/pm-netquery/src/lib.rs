//! Port of Go `service/netquery`: the connection history schema and the JSON
//! query language the UI uses (`/api/v1/netquery/query`).

pub mod query;
pub mod schema;
mod timefmt;

pub use query::{generate_sql, Param, QueryError, QueryRequest};
pub use schema::{Column, ColumnKind, CONNECTIONS};
