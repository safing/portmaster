//! Prints live socket table statistics and resolves the owner of our own
//! listening socket.
//!
//! Run with `cargo run -p pm-netstate --example lookup`.

use pm_netstate::{NetState, ProcFs, Stack};

fn main() {
    let state = NetState::new(ProcFs::default());
    let info = state.info(true);
    println!(
        "tcp4: {} connections, {} listeners",
        info.tcp4_connections.len(),
        info.tcp4_listeners.len()
    );
    println!(
        "tcp6: {} connections, {} listeners",
        info.tcp6_connections.len(),
        info.tcp6_listeners.len()
    );
    println!("udp4 binds: {}", info.udp4_binds.len());
    println!("udp6 binds: {}", info.udp6_binds.len());

    // Resolve the PID that owns a socket we just opened.
    let listener = std::net::TcpListener::bind("127.0.0.1:0").expect("bind listener");
    let port = listener.local_addr().expect("local address").port();

    let procfs = ProcFs::default();
    let (_, binds) = procfs.table(Stack::Tcp4).expect("read /proc/net/tcp");
    match binds.iter().find(|bind| bind.local.port == port) {
        Some(bind) => {
            let pid = procfs.find_pid(bind.uid, bind.inode);
            println!(
                "listener 127.0.0.1:{port} (inode {}) -> pid {pid}, this process is {}",
                bind.inode,
                std::process::id()
            );
            assert_eq!(pid, std::process::id() as i32, "own socket owner");
        }
        None => println!("listener on port {port} not visible in /proc/net/tcp"),
    }
}
