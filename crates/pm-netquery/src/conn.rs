//! A connection row (Go `netquery.Conn`) and its encoding for SQLite
//! (Go `orm.ToParamMap` with the UTC datetime encoder).

use rusqlite::types::Value as Sql;

use crate::timefmt::format_sqlite;

/// One row of the `connections` table.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Conn {
    pub id: String,
    pub profile_id: String,
    pub path: String,
    pub conn_type: String,
    pub external: bool,
    pub ip_version: u8,
    pub ip_protocol: u8,
    pub local_ip: String,
    pub local_port: u16,
    pub remote_ip: String,
    pub remote_port: u16,
    pub domain: String,
    pub country: String,
    pub asn: u32,
    pub as_owner: String,
    pub latitude: f64,
    pub longitude: f64,
    pub scope: i8,
    pub worst_verdict: i8,
    pub active_verdict: i8,
    pub firewall_verdict: i8,
    /// Unix seconds; 0 is Go's zero time, which is stored as NULL.
    pub started: i64,
    pub ended: Option<i64>,
    pub tunneled: bool,
    pub encrypted: bool,
    pub internal: bool,
    pub direction: String,
    /// Raw JSON, stored as a BLOB.
    pub extra_data: Vec<u8>,
    pub allowed: Option<bool>,
    pub profile_revision: i64,
    pub exit_node: Option<String>,
    pub bytes_received: u64,
    pub bytes_sent: u64,
    pub active: bool,
    pub profile_name: String,
}

fn text(s: &str) -> Sql {
    Sql::Text(s.to_string())
}

fn int(i: impl Into<i64>) -> Sql {
    Sql::Integer(i.into())
}

fn time(unix: i64) -> Sql {
    // Go's DatetimeEncoder stores the zero time as NULL; Go code never sets
    // `started` to the zero time, and unix 0 is the closest stand-in here.
    if unix == 0 {
        Sql::Null
    } else {
        Sql::Text(format_sqlite(unix))
    }
}

impl Conn {
    /// Column values for the upsert, sorted by column name, without the byte
    /// counters (those are only written by `update_bandwidth`).
    pub fn params(&self) -> Vec<(&'static str, Sql)> {
        let mut out = vec![
            ("active", int(self.active)),
            ("allowed", self.allowed.map_or(Sql::Null, int)),
            ("as_owner", text(&self.as_owner)),
            ("asn", int(self.asn)),
            ("country", text(&self.country)),
            ("direction", text(&self.direction)),
            ("domain", text(&self.domain)),
            ("encrypted", int(self.encrypted)),
            ("ended", self.ended.map_or(Sql::Null, time)),
            ("exit_node", self.exit_node.as_deref().map_or(Sql::Null, text)),
            ("external", int(self.external)),
            ("extra_data", Sql::Blob(self.extra_data.clone())),
            ("firewall_verdict", int(self.firewall_verdict)),
            ("id", text(&self.id)),
            ("internal", int(self.internal)),
            ("ip_protocol", int(self.ip_protocol)),
            ("ip_version", int(self.ip_version)),
            ("latitude", Sql::Real(self.latitude)),
            ("local_ip", text(&self.local_ip)),
            ("local_port", int(self.local_port)),
            ("longitude", Sql::Real(self.longitude)),
            ("path", text(&self.path)),
            ("profile", text(&self.profile_id)),
            ("profile_name", text(&self.profile_name)),
            ("profile_revision", int(self.profile_revision)),
            ("remote_ip", text(&self.remote_ip)),
            ("remote_port", int(self.remote_port)),
            ("scope", int(self.scope)),
            ("started", time(self.started)),
            ("tunneled", int(self.tunneled)),
            ("type", text(&self.conn_type)),
            ("verdict", int(self.active_verdict)),
            ("worst_verdict", int(self.worst_verdict)),
        ];
        out.sort_by_key(|(k, _)| *k);
        out
    }
}
