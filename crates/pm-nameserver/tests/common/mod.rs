//! Shared fakes for the nameserver tests.
#![allow(dead_code)]

use std::net::{IpAddr, Ipv4Addr, SocketAddr};
use std::str::FromStr;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use async_trait::async_trait;
use hickory_proto::op::{Message, MessageType, OpCode, Query as HQuery};
use hickory_proto::rr::rdata::A;
use hickory_proto::rr::{DNSClass, Name, RData, Record, RecordType};
use pm_nameserver::{
    DnsRequestInfo, DnsResolve, FilterOutcome, FirewallHook, Nameserver, NameserverDeps,
    RequestAttributor, Responder, ServerEnv,
};
use pm_network::{Connection, SharedConnection, Verdict};
use pm_resolver::{IpScope, Query, QueryError, RRCache, ResolveFailure, ResolverInfo};
use tokio::net::UdpSocket;
use tokio_util::sync::CancellationToken;

pub type ResolveFn = Box<dyn Fn(&Query) -> Result<RRCache, ResolveFailure> + Send + Sync>;

pub struct FakeResolver {
    pub handler: Mutex<ResolveFn>,
    pub calls: AtomicUsize,
    pub alt: Mutex<Option<RRCache>>,
}

impl FakeResolver {
    pub fn new(handler: ResolveFn) -> Arc<FakeResolver> {
        Arc::new(FakeResolver {
            handler: Mutex::new(handler),
            calls: AtomicUsize::new(0),
            alt: Mutex::new(None),
        })
    }

    pub fn answering() -> Arc<FakeResolver> {
        FakeResolver::new(Box::new(|q| Ok(rr_a(&q.fqdn, Ipv4Addr::new(192, 0, 2, 1)))))
    }

    pub fn failing(error: QueryError) -> Arc<FakeResolver> {
        FakeResolver::new(Box::new(move |_| Err(error.clone().into())))
    }
}

#[async_trait]
impl DnsResolve for FakeResolver {
    async fn resolve(
        &self,
        q: Query,
        _cancel: &CancellationToken,
    ) -> Result<RRCache, ResolveFailure> {
        self.calls.fetch_add(1, Ordering::SeqCst);
        (self.handler.lock().unwrap())(&q)
    }

    fn get_rr_cache(&self, _domain: &str, _qtype: RecordType) -> Option<RRCache> {
        self.alt.lock().unwrap().clone()
    }
}

pub fn resolver_info() -> ResolverInfo {
    ResolverInfo {
        name: "Fake".into(),
        r#type: "dns".into(),
        source: "config".into(),
        ip: Some(IpAddr::V4(Ipv4Addr::new(192, 0, 2, 53))),
        domain: String::new(),
        ip_scope: IpScope::Global,
        port: 53,
    }
}

pub fn rr_a(domain: &str, ip: Ipv4Addr) -> RRCache {
    RRCache {
        domain: domain.to_string(),
        question: RecordType::A,
        rcode: 0,
        answer: vec![Record::from_rdata(
            Name::from_str(domain).unwrap(),
            60,
            RData::A(A(ip)),
        )],
        ns: vec![],
        extra: vec![],
        expires: pm_resolver::rrcache::now_unix() + 60,
        resolver: Some(resolver_info()),
        served_from_cache: false,
        requesting_new: false,
        is_backup: false,
        filtered: false,
        filtered_entries: vec![],
        modified: 0,
    }
}

#[derive(Default)]
pub struct HookLog {
    pub filter_connection: AtomicUsize,
    pub filter_resolved: AtomicUsize,
    pub update_ips: AtomicUsize,
    pub saved_open: AtomicUsize,
    pub saved_conn: AtomicUsize,
}

/// A hook that sets configurable verdicts.
pub struct FakeHook {
    pub log: HookLog,
    pub pre: Mutex<Option<(Verdict, String)>>,
    pub pre_responder: Mutex<Option<Arc<dyn Responder>>>,
    pub post: Mutex<Option<(Verdict, String)>>,
    pub post_drops_cache: Mutex<bool>,
}

impl FakeHook {
    pub fn new() -> Arc<FakeHook> {
        Arc::new(FakeHook {
            log: HookLog::default(),
            pre: Mutex::new(None),
            pre_responder: Mutex::new(None),
            post: Mutex::new(None),
            post_drops_cache: Mutex::new(false),
        })
    }
}

#[async_trait]
impl FirewallHook for FakeHook {
    async fn filter_connection(&self, conn: &SharedConnection) -> FilterOutcome {
        self.log.filter_connection.fetch_add(1, Ordering::SeqCst);
        if let Some((verdict, reason)) = self.pre.lock().unwrap().clone() {
            conn.lock().unwrap().set_verdict(verdict, &reason, "", None);
        }
        FilterOutcome {
            responder: self.pre_responder.lock().unwrap().clone(),
            extra_rrs: vec![],
        }
    }

    async fn filter_resolved_dns(
        &self,
        conn: &SharedConnection,
        _q: &Query,
        rr_cache: RRCache,
    ) -> (Option<RRCache>, FilterOutcome) {
        self.log.filter_resolved.fetch_add(1, Ordering::SeqCst);
        if let Some((verdict, reason)) = self.post.lock().unwrap().clone() {
            conn.lock().unwrap().set_verdict(verdict, &reason, "", None);
        }
        let cache = if *self.post_drops_cache.lock().unwrap() {
            None
        } else {
            Some(rr_cache)
        };
        (cache, FilterOutcome::default())
    }

    fn update_ips_and_cnames(&self, _q: &Query, _rr: &RRCache, _conn: &SharedConnection) {
        self.log.update_ips.fetch_add(1, Ordering::SeqCst);
    }

    fn save_open_dns_request(&self, _q: &Query, _rr: &RRCache, _conn: &SharedConnection) {
        self.log.saved_open.fetch_add(1, Ordering::SeqCst);
    }

    fn save_connection(&self, _conn: &SharedConnection) {
        self.log.saved_conn.fetch_add(1, Ordering::SeqCst);
    }
}

pub struct FakeAttributor {
    pub requests: Mutex<Vec<DnsRequestInfo>>,
    pub created: Mutex<Vec<SharedConnection>>,
    pub fail: bool,
    pub system_resolver: bool,
}

impl FakeAttributor {
    pub fn new() -> Arc<FakeAttributor> {
        FakeAttributor::with(false, false)
    }

    pub fn with(fail: bool, system_resolver: bool) -> Arc<FakeAttributor> {
        Arc::new(FakeAttributor {
            requests: Mutex::new(vec![]),
            created: Mutex::new(vec![]),
            fail,
            system_resolver,
        })
    }

    pub fn conn(&self) -> SharedConnection {
        self.created.lock().unwrap()[0].clone()
    }
}

#[async_trait]
impl RequestAttributor for FakeAttributor {
    async fn attribute(&self, request: &DnsRequestInfo) -> Result<SharedConnection, String> {
        self.requests.lock().unwrap().push(request.clone());
        if self.fail {
            return Err("no profile".into());
        }
        let conn = Connection {
            id: request.conn_id.clone(),
            external: !request.local,
            ..Connection::default()
        };
        let conn = Arc::new(Mutex::new(conn));
        self.created.lock().unwrap().push(conn.clone());
        Ok(conn)
    }

    fn is_system_resolver(&self, _conn: &Connection) -> bool {
        self.system_resolver
    }
}

pub struct FakeEnv {
    pub local: bool,
    pub network_service: bool,
    pub ipv6: bool,
    pub hostname: String,
}

impl Default for FakeEnv {
    fn default() -> FakeEnv {
        FakeEnv {
            local: true,
            network_service: false,
            ipv6: true,
            hostname: "testhost.".into(),
        }
    }
}

impl ServerEnv for FakeEnv {
    fn hostname(&self) -> String {
        self.hostname.clone()
    }
    fn is_my_ip(&self, _ip: IpAddr) -> Result<bool, String> {
        Ok(self.local)
    }
    fn network_service_mode(&self) -> bool {
        self.network_service
    }
    fn ipv6_enabled(&self) -> bool {
        self.ipv6
    }
}

pub struct Rig {
    pub ns: Arc<Nameserver>,
    pub resolver: Arc<FakeResolver>,
    pub hook: Arc<FakeHook>,
    pub attributor: Arc<FakeAttributor>,
}

pub fn rig(resolver: Arc<FakeResolver>) -> Rig {
    rig_with(resolver, FakeEnv::default(), FakeAttributor::new())
}

pub fn rig_with(resolver: Arc<FakeResolver>, env: FakeEnv, attributor: Arc<FakeAttributor>) -> Rig {
    let hook = FakeHook::new();
    let ns = Nameserver::new(NameserverDeps {
        resolver: resolver.clone(),
        attributor: attributor.clone(),
        hook: hook.clone(),
        env: Arc::new(env),
    });
    Rig {
        ns: Arc::new(ns),
        resolver,
        hook,
        attributor,
    }
}

pub fn query_message(name: &str, qtype: RecordType) -> Message {
    let mut msg = Message::new(0x1234, MessageType::Query, OpCode::Query);
    msg.metadata.recursion_desired = true;
    msg.add_query(HQuery::query(Name::from_ascii(name).unwrap(), qtype));
    msg
}

pub fn remote() -> SocketAddr {
    SocketAddr::from(([127, 0, 0, 1], 40000))
}

/// Sends a datagram and waits for one reply; `None` if nothing came back.
pub async fn exchange(server: SocketAddr, payload: &[u8], wait: Duration) -> Option<Message> {
    let bind: SocketAddr = if server.is_ipv4() {
        "127.0.0.1:0".parse().unwrap()
    } else {
        "[::1]:0".parse().unwrap()
    };
    let socket = UdpSocket::bind(bind).await.unwrap();
    socket.send_to(payload, server).await.unwrap();
    let mut buf = vec![0u8; 65535];
    match tokio::time::timeout(wait, socket.recv_from(&mut buf)).await {
        Ok(Ok((n, _))) => Some(Message::from_vec(&buf[..n]).unwrap()),
        _ => None,
    }
}

pub fn txt_extras(reply: &Message) -> Vec<String> {
    reply
        .additionals
        .iter()
        .filter_map(|r| match &r.data {
            RData::TXT(txt) => Some(
                txt.txt_data
                    .iter()
                    .map(|d| String::from_utf8_lossy(d).into_owned())
                    .collect::<String>(),
            ),
            _ => None,
        })
        .collect()
}

pub fn answer_ips(reply: &Message) -> Vec<IpAddr> {
    reply
        .answers
        .iter()
        .filter_map(|r| match &r.data {
            RData::A(a) => Some(IpAddr::V4(a.0)),
            RData::AAAA(a) => Some(IpAddr::V6(a.0)),
            _ => None,
        })
        .collect()
}

pub fn class_in() -> DNSClass {
    DNSClass::IN
}
