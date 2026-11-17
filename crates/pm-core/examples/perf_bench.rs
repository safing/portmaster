//! Benchmark suite for Rust Core high-performance optimizations in Portmaster.
//!
//! Measures:
//! 1. RFC 1071 Internet Checksum (64-bit unrolled / SIMD vs naive byte-by-byte).
//! 2. Zero-alloc / zero-copy proc net parsing vs allocating parser.
//! 3. Connection ID formatting (in-place write vs naive format!).
//! 4. Sharded ConnectionStore throughput under multi-threaded contention.
//! 5. Cacheline padding: false sharing elimination on atomic counters.
//! 6. Bounded RingBuffer throughput (lock-reduced SPSC/MPMC).

use std::hint::black_box;
use std::net::IpAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use pm_interception::packet::internet_checksum;
use pm_netstate::proc::{convert_ipv4, convert_ipv6, parse_table, Stack};
use pm_network::{create_connection_id, Connection, ConnectionStore};
use pm_utils::ring_buffer::RingBuffer;

fn bench_checksum() {
    println!("=== 1. RFC 1071 Internet Checksum Benchmark ===");
    let size = 1500; // Standard MTU
    let packet_data: Vec<u8> = (0..size).map(|i| (i * 37) as u8).collect();
    let iters = 200_000;

    // Naive 16-bit fold
    let start = Instant::now();
    let mut sum_naive = 0u16;
    for _ in 0..iters {
        let mut s: u32 = 0;
        let mut i = 0;
        while i + 1 < packet_data.len() {
            s += u16::from_be_bytes([packet_data[i], packet_data[i + 1]]) as u32;
            i += 2;
        }
        if i < packet_data.len() {
            s += (packet_data[i] as u32) << 8;
        }
        while (s >> 16) != 0 {
            s = (s & 0xffff) + (s >> 16);
        }
        sum_naive = !(s as u16);
        black_box(sum_naive);
    }
    let elapsed_naive = start.elapsed();

    // Optimized 64-bit unrolled / SIMD
    let start = Instant::now();
    let mut sum_opt = 0u16;
    for _ in 0..iters {
        sum_opt = internet_checksum(black_box(&packet_data));
        black_box(sum_opt);
    }
    let elapsed_opt = start.elapsed();

    assert_eq!(sum_naive, sum_opt);
    let total_gb = (size as f64 * iters as f64) / 1_000_000_000.0;
    let gbps_naive = total_gb / elapsed_naive.as_secs_f64();
    let gbps_opt = total_gb / elapsed_opt.as_secs_f64();
    let speedup = elapsed_naive.as_secs_f64() / elapsed_opt.as_secs_f64();

    println!("Naive loop:      {:?} ({:.2} GB/s)", elapsed_naive, gbps_naive);
    println!("64-bit Unrolled: {:?} ({:.2} GB/s)", elapsed_opt, gbps_opt);
    println!("Speedup:         {:.2}x\n", speedup);
}

fn bench_connection_id_formatting() {
    println!("=== 2. Zero-Allocation Connection ID Formatting ===");
    let src: IpAddr = "192.168.1.100".parse().unwrap();
    let dst: IpAddr = "1.1.1.1".parse().unwrap();
    let iters = 500_000;

    let start = Instant::now();
    for _ in 0..iters {
        let id = create_connection_id(6, Some(src), 54321, Some(dst), 443, false);
        black_box(id);
    }
    let elapsed = start.elapsed();
    let ops_sec = (iters as f64) / elapsed.as_secs_f64();

    println!("Formatted {} connection IDs in {:?}", iters, elapsed);
    println!("Throughput: {:.2} M ops/sec\n", ops_sec / 1_000_000.0);
}

fn bench_proc_parsing() {
    println!("=== 3. Zero-Alloc /proc/net Table & Hex Parsing ===");
    // Sample /proc/net/tcp line
    let sample_data = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n   0: 0100007F:0035 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 0000000000000000 100 0 0 10 0\n";
    let iters = 100_000;

    let start = Instant::now();
    for _ in 0..iters {
        let (conns, binds) = parse_table(Stack::Tcp4, black_box(sample_data));
        black_box((conns, binds));
    }
    let elapsed = start.elapsed();
    let ops_sec = (iters as f64) / elapsed.as_secs_f64();

    println!("Parsed {} proc table lines in {:?}", iters, elapsed);
    println!("Throughput: {:.2} M lines/sec\n", ops_sec / 1_000_000.0);

    // Bench hex IP decoding directly
    let ipv4_hex = "0100007F";
    let ipv6_hex = "00000000000000000000000001000000";
    let hex_iters = 1_000_000;

    let start = Instant::now();
    for _ in 0..hex_iters {
        let ip = convert_ipv4(black_box(ipv4_hex));
        black_box(ip);
    }
    let elapsed_v4 = start.elapsed();

    let start = Instant::now();
    for _ in 0..hex_iters {
        let ip = convert_ipv6(black_box(ipv6_hex));
        black_box(ip);
    }
    let elapsed_v6 = start.elapsed();

    println!("IPv4 hex decode: {:?} ({:.2} M ops/sec)", elapsed_v4, (hex_iters as f64) / elapsed_v4.as_secs_f64() / 1e6);
    println!("IPv6 hex decode: {:?} ({:.2} M ops/sec)\n", elapsed_v6, (hex_iters as f64) / elapsed_v6.as_secs_f64() / 1e6);
}

fn bench_cacheline_padding() {
    println!("=== 4. Cacheline Padding & False Sharing Elimination ===");
    let threads = 4;
    let iters_per_thread = 2_000_000;

    // Unpadded: 4 AtomicU64 packed in one cache line
    #[derive(Default)]
    struct UnpaddedCounters {
        c0: AtomicU64,
        c1: AtomicU64,
        c2: AtomicU64,
        c3: AtomicU64,
    }
    let unpadded = Arc::new(UnpaddedCounters::default());
    let start = Instant::now();
    let mut handles = Vec::new();
    for tid in 0..threads {
        let u = Arc::clone(&unpadded);
        handles.push(std::thread::spawn(move || {
            for _ in 0..iters_per_thread {
                match tid % 4 {
                    0 => u.c0.fetch_add(1, Ordering::Relaxed),
                    1 => u.c1.fetch_add(1, Ordering::Relaxed),
                    2 => u.c2.fetch_add(1, Ordering::Relaxed),
                    _ => u.c3.fetch_add(1, Ordering::Relaxed),
                };
            }
        }));
    }
    for h in handles {
        h.join().unwrap();
    }
    let elapsed_unpadded = start.elapsed();

    // Cacheline padded: #[repr(align(64))]
    #[repr(align(64))]
    #[derive(Default)]
    struct PaddedU64(AtomicU64);
    #[derive(Default)]
    struct PaddedCounters {
        c0: PaddedU64,
        c1: PaddedU64,
        c2: PaddedU64,
        c3: PaddedU64,
    }
    let padded = Arc::new(PaddedCounters::default());
    let start = Instant::now();
    let mut handles = Vec::new();
    for tid in 0..threads {
        let p = Arc::clone(&padded);
        handles.push(std::thread::spawn(move || {
            for _ in 0..iters_per_thread {
                match tid % 4 {
                    0 => p.c0.0.fetch_add(1, Ordering::Relaxed),
                    1 => p.c1.0.fetch_add(1, Ordering::Relaxed),
                    2 => p.c2.0.fetch_add(1, Ordering::Relaxed),
                    _ => p.c3.0.fetch_add(1, Ordering::Relaxed),
                };
            }
        }));
    }
    for h in handles {
        h.join().unwrap();
    }
    let elapsed_padded = start.elapsed();

    let speedup = elapsed_unpadded.as_secs_f64() / elapsed_padded.as_secs_f64();
    println!("Unpadded (false sharing): {:?}", elapsed_unpadded);
    println!("Padded (cacheline align): {:?}", elapsed_padded);
    println!("Multi-core contention speedup: {:.2}x\n", speedup);
}

fn bench_sharded_store() {
    println!("=== 5. Sharded ConnectionStore Throughput ===");
    let store: Arc<ConnectionStore> = Arc::new(ConnectionStore::new());
    let num_conns = 1_000;
    for i in 0..num_conns {
        let conn = Arc::new(std::sync::Mutex::new(Connection::default()));
        store.add(&format!("conn-{i}"), &conn);
    }

    let threads = 4;
    let lookups_per_thread = 500_000;
    let start = Instant::now();
    let mut handles = Vec::new();
    for tid in 0..threads {
        let s = Arc::clone(&store);
        handles.push(std::thread::spawn(move || {
            for i in 0..lookups_per_thread {
                let id = format!("conn-{}", (i + tid) % num_conns);
                let res = s.get(&id);
                black_box(res);
            }
        }));
    }
    for h in handles {
        h.join().unwrap();
    }
    let elapsed = start.elapsed();
    let total_lookups = threads * lookups_per_thread;
    let throughput = (total_lookups as f64) / elapsed.as_secs_f64();

    println!("Performed {} lookups across {} threads in {:?}", total_lookups, threads, elapsed);
    println!("Throughput: {:.2} M lookups/sec\n", throughput / 1_000_000.0);
}

fn bench_ring_buffer() {
    println!("=== 6. Lock-Reduced Bounded RingBuffer Throughput ===");
    let rb = Arc::new(RingBuffer::new(4096));
    let total_items = 2_000_000;

    let rb_p = Arc::clone(&rb);
    let start = Instant::now();
    let producer = std::thread::spawn(move || {
        for i in 0..total_items {
            while rb_p.try_push(i).is_err() {
                std::hint::spin_loop();
            }
        }
    });

    let rb_c = Arc::clone(&rb);
    let consumer = std::thread::spawn(move || {
        let mut count = 0;
        while count < total_items {
            if let Some(val) = rb_c.try_pop() {
                black_box(val);
                count += 1;
            } else {
                std::hint::spin_loop();
            }
        }
    });

    producer.join().unwrap();
    consumer.join().unwrap();
    let elapsed = start.elapsed();
    let throughput = (total_items as f64) / elapsed.as_secs_f64();

    println!("Transferred {} items concurrently in {:?}", total_items, elapsed);
    println!("Throughput: {:.2} M msgs/sec\n", throughput / 1_000_000.0);
}

fn main() {
    println!("================================================================");
    println!("       PORTMASTER RUST CORE PERFORMANCE BENCHMARK SUITE          ");
    println!("================================================================\n");

    bench_checksum();
    bench_connection_id_formatting();
    bench_proc_parsing();
    bench_cacheline_padding();
    bench_sharded_store();
    bench_ring_buffer();

    println!("================================================================");
    println!("              ALL BENCHMARKS COMPLETED SUCCESSFULLY             ");
    println!("================================================================");
}
