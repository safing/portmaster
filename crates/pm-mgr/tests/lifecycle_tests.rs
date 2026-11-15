use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

use pm_mgr::{EventMgr, Group, Manager, Module, ModuleState};

struct TestModule {
    name: String,
    start_err: bool,
    stop_err: bool,
    events: Arc<Mutex<Vec<String>>>,
    start_worker: bool,
    worker_stopped: Arc<AtomicBool>,
}

impl TestModule {
    fn new(name: impl Into<String>, events: Arc<Mutex<Vec<String>>>) -> Self {
        Self {
            name: name.into(),
            start_err: false,
            stop_err: false,
            events,
            start_worker: false,
            worker_stopped: Arc::new(AtomicBool::new(false)),
        }
    }

    fn with_start_err(mut self, fail: bool) -> Self {
        self.start_err = fail;
        self
    }

    fn with_stop_err(mut self, fail: bool) -> Self {
        self.stop_err = fail;
        self
    }

    fn with_worker(mut self, worker_stopped: Arc<AtomicBool>) -> Self {
        self.start_worker = true;
        self.worker_stopped = worker_stopped;
        self
    }
}

#[async_trait::async_trait]
impl Module for TestModule {
    fn name(&self) -> &str {
        &self.name
    }

    async fn start(&self, mgr: &Manager) -> anyhow::Result<()> {
        {
            let mut ev = self.events.lock().unwrap();
            ev.push(format!("start:{}", self.name));
        }

        if self.start_worker {
            let cancel = mgr.cancel_token();
            let stopped = Arc::clone(&self.worker_stopped);
            mgr.go(format!("worker-{}", self.name), async move {
                cancel.cancelled().await;
                stopped.store(true, Ordering::SeqCst);
            });
        }

        if self.start_err {
            anyhow::bail!("failed to start {}", self.name);
        }

        Ok(())
    }

    async fn stop(&self, _mgr: &Manager) -> anyhow::Result<()> {
        {
            let mut ev = self.events.lock().unwrap();
            ev.push(format!("stop:{}", self.name));
        }

        if self.stop_err {
            anyhow::bail!("failed to stop {}", self.name);
        }

        Ok(())
    }
}

#[tokio::test]
async fn test_start_order() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mod_a = Arc::new(TestModule::new("mod_a", Arc::clone(&events)));
    let mod_b = Arc::new(TestModule::new("mod_b", Arc::clone(&events)));
    let mod_c = Arc::new(TestModule::new("mod_c", Arc::clone(&events)));

    let group = Group::new(vec![mod_a, mod_b, mod_c]);
    assert_eq!(group.state(), ModuleState::Off);

    group
        .start()
        .await
        .expect("group should start successfully");
    assert_eq!(group.state(), ModuleState::Running);
    assert!(group.is_running());

    let recorded = events.lock().unwrap().clone();
    assert_eq!(recorded, vec!["start:mod_a", "start:mod_b", "start:mod_c"]);
}

#[tokio::test]
async fn test_reverse_stop() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mod_a = Arc::new(TestModule::new("mod_a", Arc::clone(&events)));
    let mod_b = Arc::new(TestModule::new("mod_b", Arc::clone(&events)));
    let mod_c = Arc::new(TestModule::new("mod_c", Arc::clone(&events)));

    let group = Group::new(vec![mod_a, mod_b, mod_c]);
    group.start().await.expect("start must succeed");

    events.lock().unwrap().clear();

    group.stop().await.expect("stop must succeed");
    assert_eq!(group.state(), ModuleState::Off);
    assert!(group.is_stopped().unwrap());

    let recorded = events.lock().unwrap().clone();
    assert_eq!(recorded, vec!["stop:mod_c", "stop:mod_b", "stop:mod_a"]);
}

#[tokio::test]
async fn test_rollback_on_failure() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mod_a = Arc::new(TestModule::new("mod_a", Arc::clone(&events)));
    let mod_b = Arc::new(TestModule::new("mod_b", Arc::clone(&events)).with_start_err(true));
    let mod_c = Arc::new(TestModule::new("mod_c", Arc::clone(&events)));

    let group = Group::new(vec![mod_a, mod_b, mod_c]);
    let res = group.start().await;
    assert!(res.is_err(), "group.start() should return error");

    // Group should roll back and end in Off state (since stops succeeded)
    assert_eq!(group.state(), ModuleState::Off);

    let recorded = events.lock().unwrap().clone();
    assert_eq!(
        recorded,
        vec!["start:mod_a", "start:mod_b", "stop:mod_b", "stop:mod_a",]
    );
}

#[tokio::test]
async fn test_worker_cancellation_on_stop() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let worker_stopped = Arc::new(AtomicBool::new(false));
    let mod_a = Arc::new(
        TestModule::new("worker_mod", Arc::clone(&events)).with_worker(Arc::clone(&worker_stopped)),
    );

    let group = Group::new(vec![mod_a]);
    group.start().await.expect("start must succeed");

    // Worker should still be running
    tokio::time::sleep(Duration::from_millis(20)).await;
    assert!(!worker_stopped.load(Ordering::SeqCst));

    // Stop group - cancels worker token
    group.stop().await.expect("stop must succeed");

    assert!(
        worker_stopped.load(Ordering::SeqCst),
        "worker must have received cancellation and stopped"
    );
}

#[tokio::test]
async fn test_panicking_worker_survives() {
    let mgr = Manager::new("panic_test_mgr");

    let handle = mgr.go("panicking_worker", async {
        if true {
            panic!("deliberate worker panic for testing");
        }
    });

    // Supervisor awaits the worker, catches panic, and logs it
    handle.await.expect("supervisor should complete normally");

    // Manager and process survive; healthy workers continue to function
    let healthy_executed = Arc::new(AtomicBool::new(false));
    let healthy_flag = Arc::clone(&healthy_executed);
    let healthy_handle = mgr.go("healthy_worker", async move {
        healthy_flag.store(true, Ordering::SeqCst);
    });

    healthy_handle.await.expect("healthy worker should finish");
    assert!(healthy_executed.load(Ordering::SeqCst));
}

#[tokio::test]
async fn test_repeat_runs_multiple_times_then_stops_on_cancel() {
    let mgr = Manager::new("repeat_test_mgr");
    let count = Arc::new(AtomicUsize::new(0));

    let count_clone = Arc::clone(&count);
    let repeat_handle = mgr.repeat("counter_worker", Duration::from_millis(15), move || {
        let c = Arc::clone(&count_clone);
        async move {
            c.fetch_add(1, Ordering::SeqCst);
        }
    });

    // Allow it to run several times
    tokio::time::sleep(Duration::from_millis(70)).await;
    let initial_count = count.load(Ordering::SeqCst);
    assert!(
        initial_count >= 2,
        "repeat worker should run at least 2 times, got {initial_count}"
    );

    // Cancel manager
    mgr.cancel();

    // Await repeat supervisor task completion
    tokio::time::timeout(Duration::from_millis(100), repeat_handle)
        .await
        .expect("repeat task should terminate promptly upon cancellation")
        .expect("join should succeed");

    let stopped_count = count.load(Ordering::SeqCst);

    // Wait a bit more and verify no further increments occurred
    tokio::time::sleep(Duration::from_millis(40)).await;
    let final_count = count.load(Ordering::SeqCst);
    assert_eq!(
        stopped_count, final_count,
        "repeat worker must not increment after cancellation"
    );
}

#[tokio::test]
async fn test_events_reach_subscribers_and_callbacks() {
    let event_mgr = EventMgr::<String>::new("test_bus");

    let mut sub1 = event_mgr.subscribe("sub1", 10);
    let mut sub2 = event_mgr.subscribe("sub2", 10);

    let callback_received = Arc::new(Mutex::new(Vec::new()));
    let cb_rec = Arc::clone(&callback_received);
    event_mgr.add_callback("record_cb", move |ev: String| {
        cb_rec.lock().unwrap().push(ev);
        Ok(false)
    });

    event_mgr.submit("event_alpha".to_string());
    event_mgr.submit("event_beta".to_string());

    // Check subscribers
    let sub1_ev1 = sub1.recv().await.expect("sub1 receives event 1");
    let sub1_ev2 = sub1.recv().await.expect("sub1 receives event 2");
    assert_eq!(sub1_ev1, "event_alpha");
    assert_eq!(sub1_ev2, "event_beta");

    let sub2_ev1 = sub2.recv().await.expect("sub2 receives event 1");
    let sub2_ev2 = sub2.recv().await.expect("sub2 receives event 2");
    assert_eq!(sub2_ev1, "event_alpha");
    assert_eq!(sub2_ev2, "event_beta");

    // Check callback
    let cb_events = callback_received.lock().unwrap().clone();
    assert_eq!(cb_events, vec!["event_alpha", "event_beta"]);
}

#[tokio::test]
async fn test_cancel_true_callback_is_removed() {
    let event_mgr = EventMgr::<i32>::new("cancel_bus");

    let calls = Arc::new(AtomicUsize::new(0));
    let calls_clone = Arc::clone(&calls);

    // Callback that returns Ok(true) to cancel itself after first run
    event_mgr.add_callback("one_shot_cb", move |_val: i32| {
        calls_clone.fetch_add(1, Ordering::SeqCst);
        Ok(true) // cancel = true
    });

    assert_eq!(event_mgr.callback_count(), 1);

    // First submit triggers callback and causes cancellation
    event_mgr.submit(100);
    assert_eq!(calls.load(Ordering::SeqCst), 1);
    assert_eq!(event_mgr.callback_count(), 0);

    // Second submit must not trigger the removed callback
    event_mgr.submit(200);
    assert_eq!(calls.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn test_delay_runs_and_cancels() {
    let mgr = Manager::new("delay_mgr");
    let executed = Arc::new(AtomicBool::new(false));

    let exec_clone = Arc::clone(&executed);
    let handle = mgr.delay("delayed_task", Duration::from_millis(20), move || {
        let e = Arc::clone(&exec_clone);
        async move {
            e.store(true, Ordering::SeqCst);
        }
    });

    handle.await.expect("delayed task supervisor finishes");
    assert!(executed.load(Ordering::SeqCst));

    // Test cancellation before delay fires
    let cancelled_executed = Arc::new(AtomicBool::new(false));
    let ce_clone = Arc::clone(&cancelled_executed);
    mgr.reset();
    let handle2 = mgr.delay("cancelled_delay", Duration::from_millis(50), move || {
        let e = Arc::clone(&ce_clone);
        async move {
            e.store(true, Ordering::SeqCst);
        }
    });

    mgr.cancel();
    handle2.await.expect("handle2 finishes");
    assert!(
        !cancelled_executed.load(Ordering::SeqCst),
        "task cancelled during delay should not execute"
    );
}

#[tokio::test]
async fn test_group_invalid_state_and_double_calls() {
    let events = Arc::new(Mutex::new(Vec::new()));
    let mod_a = Arc::new(TestModule::new("mod_a", Arc::clone(&events)));
    let group = Group::new(vec![mod_a]);

    // Double stop when off is Ok
    group.stop().await.expect("stop when off should be ok");

    // Start group
    group.start().await.expect("start ok");

    // Double start when running is Ok
    group
        .start()
        .await
        .expect("start when running should be ok");

    // Stop group
    group.stop().await.expect("stop ok");

    // Failure on stop makes state Invalid
    let mod_failing =
        Arc::new(TestModule::new("mod_fail", Arc::clone(&events)).with_stop_err(true));
    let group_fail = Group::new(vec![mod_failing]);
    group_fail.start().await.expect("start succeeds");
    let stop_res = group_fail.stop().await;
    assert!(stop_res.is_err());
    assert_eq!(group_fail.state(), ModuleState::Invalid);

    // Starting or stopping an invalid group returns error
    assert!(group_fail.start().await.is_err());
    assert!(group_fail.stop().await.is_err());
    assert!(group_fail.is_stopped().is_err());
}
