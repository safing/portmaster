//! Port of Go `service/netquery`: the connection history schema and the JSON
//! query language the UI uses (`/api/v1/netquery/query`) and the database
//! with live (in-memory) and history connections.

pub mod api;
pub mod conn;
pub mod db;
pub mod query;
pub mod schema;
mod timefmt;

pub use conn::Conn;
pub use db::{Database, DbError, Row};
pub use query::{chart_where_clause, generate_sql, Param, QueryError, QueryRequest};
pub use schema::{Column, ColumnKind, CONNECTIONS};
