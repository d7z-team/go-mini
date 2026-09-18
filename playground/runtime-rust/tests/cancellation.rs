use mini_go::ffi::Cancellation;
use std::{
    future::Future,
    pin::Pin,
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    task::{Context, Wake, Waker},
};

struct Counter(AtomicUsize);
impl Wake for Counter {
    fn wake(self: Arc<Self>) {
        self.0.fetch_add(1, Ordering::SeqCst);
    }
}

#[test]
fn cancellation_replaces_wakers_and_dropped_subscriptions_do_not_wake() {
    let cancellation = Cancellation::default();
    let first = Arc::new(Counter(AtomicUsize::new(0)));
    let second = Arc::new(Counter(AtomicUsize::new(0)));
    let first_waker = Waker::from(first.clone());
    let second_waker = Waker::from(second.clone());
    let mut future = cancellation.cancelled();
    assert!(
        Pin::new(&mut future)
            .poll(&mut Context::from_waker(&first_waker))
            .is_pending()
    );
    assert!(
        Pin::new(&mut future)
            .poll(&mut Context::from_waker(&second_waker))
            .is_pending()
    );
    let mut dropped = cancellation.cancelled();
    assert!(
        Pin::new(&mut dropped)
            .poll(&mut Context::from_waker(&first_waker))
            .is_pending()
    );
    drop(dropped);
    cancellation.cancel();
    cancellation.cancel();
    assert_eq!(first.0.load(Ordering::SeqCst), 0);
    assert_eq!(second.0.load(Ordering::SeqCst), 1);
    assert!(
        Pin::new(&mut future)
            .poll(&mut Context::from_waker(&second_waker))
            .is_ready()
    );
}

#[test]
fn concurrent_subscription_and_cancel_cannot_lose_wakeup() {
    for _ in 0..64 {
        let cancellation = Cancellation::default();
        let signal = cancellation.clone();
        let barrier = Arc::new(std::sync::Barrier::new(2));
        let ready = barrier.clone();
        let worker = std::thread::spawn(move || {
            ready.wait();
            signal.cancel();
        });
        let counter = Arc::new(Counter(AtomicUsize::new(0)));
        let waker = Waker::from(counter.clone());
        let mut future = cancellation.cancelled();
        barrier.wait();
        let poll = Pin::new(&mut future).poll(&mut Context::from_waker(&waker));
        worker.join().unwrap();
        assert!(poll.is_ready() || counter.0.load(Ordering::SeqCst) == 1);
        assert!(
            Pin::new(&mut future)
                .poll(&mut Context::from_waker(&waker))
                .is_ready()
        );
    }
}
