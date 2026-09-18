use mini_go::{
    error::RuntimeError,
    ffi::{Call, PendingCalls, Reply, Wake},
};
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

struct HostCall(Arc<AtomicUsize>);
impl Call for HostCall {
    fn cancel(&self) {
        self.0.fetch_add(1, Ordering::SeqCst);
    }
}

fn owned_reply(discarded: &Arc<AtomicUsize>, bytes: Vec<u8>) -> Reply {
    let discarded = discarded.clone();
    Reply::new(
        bytes,
        None,
        Some(Box::new(move || {
            discarded.fetch_add(1, Ordering::SeqCst);
        })),
    )
}

#[test]
fn delivery_receipt_and_discard_have_exclusive_terminal_ownership() {
    for (deliver, fail) in [(true, false), (false, false), (true, true)] {
        let consumed = Arc::new(AtomicUsize::new(0));
        let discarded = Arc::new(AtomicUsize::new(0));
        let receipt = consumed.clone();
        let cleanup = discarded.clone();
        let reply = Reply::new(
            vec![0, 255],
            fail.then(|| RuntimeError::new("host", "test", "failed")),
            Some(Box::new(move || {
                cleanup.fetch_add(1, Ordering::SeqCst);
            })),
        )
        .on_consumed(move || {
            receipt.fetch_add(1, Ordering::SeqCst);
        });
        if deliver {
            assert_eq!(reply.consume().is_ok(), !fail);
        } else {
            drop(reply);
        }
        assert_eq!(
            consumed.load(Ordering::SeqCst),
            usize::from(deliver && !fail)
        );
        assert_eq!(
            discarded.load(Ordering::SeqCst),
            usize::from(!deliver || fail)
        );
    }
}

#[test]
fn synchronous_completion_waits_for_start_before_delivery() {
    let wake = Arc::new(Wake::default());
    let epoch = wake.epoch();
    let mut pending = PendingCalls::new(2, 64, wake.clone());
    let discarded = Arc::new(AtomicUsize::new(0));
    let cancelled = Arc::new(AtomicUsize::new(0));
    let (id, _, completion) = pending.reserve(8, 16).unwrap();
    completion.complete(owned_reply(&discarded, vec![1, 2]));
    assert!(pending.take(id).is_none());
    assert_ne!(wake.epoch(), epoch);
    pending
        .started(id, Ok(Box::new(HostCall(cancelled.clone()))))
        .unwrap();
    assert_eq!(pending.take(id).unwrap().consume().unwrap(), [1, 2]);
    assert_eq!(pending.reserved_bytes(), 0);
    pending.close();
    assert_eq!(discarded.load(Ordering::SeqCst), 0);
    assert_eq!(cancelled.load(Ordering::SeqCst), 0);
}

#[test]
fn actual_reply_bytes_remain_reserved_through_guest_delivery() {
    let mut pending = PendingCalls::new(3, 8, Arc::new(Wake::default()));
    let discarded = Arc::new(AtomicUsize::new(0));
    let canceled = Arc::new(AtomicUsize::new(0));
    let (first, _, complete_first) = pending.reserve(2, 8).unwrap();
    let (second, _, complete_second) = pending.reserve(2, 8).unwrap();
    assert_eq!(pending.reserved_bytes(), 4);
    complete_first.complete(owned_reply(&discarded, vec![1; 4]));
    complete_second.complete(owned_reply(&discarded, vec![2]));
    assert_eq!(pending.reserved_bytes(), 8);
    assert_eq!(discarded.load(Ordering::SeqCst), 1);
    pending
        .started(first, Ok(Box::new(HostCall(canceled.clone()))))
        .unwrap();
    pending
        .started(second, Ok(Box::new(HostCall(canceled))))
        .unwrap();
    let reply = pending.take(first).unwrap();
    assert_eq!(pending.reserved_bytes(), 8);
    assert_eq!(pending.reserve(1, 8).err().unwrap().code, "boundary_limit");
    assert_eq!(reply.consume().unwrap(), [1; 4]);
    assert_eq!(pending.reserved_bytes(), 2);
    assert_eq!(
        pending.take(second).unwrap().consume().unwrap_err().code,
        "boundary_limit"
    );
    assert_eq!(pending.reserved_bytes(), 0);
    assert_eq!(discarded.load(Ordering::SeqCst), 1);
}

#[test]
fn concurrent_completions_share_one_atomic_response_budget() {
    let mut pending = PendingCalls::new(2, 4, Arc::new(Wake::default()));
    let discarded = Arc::new(AtomicUsize::new(0));
    let canceled = Arc::new(AtomicUsize::new(0));
    let (first, _, complete_first) = pending.reserve(0, 4).unwrap();
    let (second, _, complete_second) = pending.reserve(0, 4).unwrap();
    std::thread::scope(|threads| {
        threads.spawn(|| complete_first.complete(owned_reply(&discarded, vec![1; 4])));
        threads.spawn(|| complete_second.complete(owned_reply(&discarded, vec![2; 4])));
    });
    assert_eq!(pending.reserved_bytes(), 4);
    let mut delivered = 0;
    for id in [first, second] {
        pending
            .started(id, Ok(Box::new(HostCall(canceled.clone()))))
            .unwrap();
        match pending.take(id).unwrap().consume() {
            Ok(bytes) => {
                assert_eq!(bytes.len(), 4);
                delivered += 1;
            }
            Err(error) => assert_eq!(error.code, "boundary_limit"),
        }
    }
    assert_eq!(delivered, 1);
    assert_eq!(discarded.load(Ordering::SeqCst), 1);
    assert_eq!(pending.reserved_bytes(), 0);
}

#[test]
fn failed_start_discards_early_completion_and_releases_budget() {
    let mut pending = PendingCalls::new(1, 64, Arc::new(Wake::default()));
    let discarded = Arc::new(AtomicUsize::new(0));
    let (id, cancellation, completion) = pending.reserve(8, 16).unwrap();
    completion.complete(owned_reply(&discarded, vec![1]));
    assert!(
        pending
            .started(id, Err(RuntimeError::new("host", "start", "failed")))
            .is_err()
    );
    assert!(cancellation.is_cancelled());
    assert_eq!(pending.reserved_bytes(), 0);
    assert_eq!(discarded.load(Ordering::SeqCst), 1);
    assert!(pending.reserve(8, 16).is_ok());
}

#[test]
fn close_cancels_handles_and_discards_late_completion_once() {
    let mut pending = PendingCalls::new(1, 64, Arc::new(Wake::default()));
    let discarded = Arc::new(AtomicUsize::new(0));
    let cancelled = Arc::new(AtomicUsize::new(0));
    let (id, cancellation, completion) = pending.reserve(8, 16).unwrap();
    pending
        .started(id, Ok(Box::new(HostCall(cancelled.clone()))))
        .unwrap();
    pending.close();
    pending.close();
    let observed_discards = discarded.clone();
    std::thread::spawn(move || completion.complete(owned_reply(&discarded, vec![1])))
        .join()
        .unwrap();
    assert!(cancellation.is_cancelled());
    assert_eq!(observed_discards.load(Ordering::SeqCst), 1);
    assert_eq!(cancelled.load(Ordering::SeqCst), 1);
    assert_eq!(pending.pending_count(), 0);
    assert_eq!(pending.reserved_bytes(), 0);
}

#[test]
fn cancelled_start_releases_handle_and_oversized_result_is_discarded() {
    let mut pending = PendingCalls::new(1, 8, Arc::new(Wake::default()));
    let discarded = Arc::new(AtomicUsize::new(0));
    let cancelled = Arc::new(AtomicUsize::new(0));
    let (id, _, completion) = pending.reserve(4, 4).unwrap();
    assert!(pending.reserve(1, 1).is_err());
    completion.complete(owned_reply(&discarded, vec![0; 5]));
    pending
        .started(id, Ok(Box::new(HostCall(cancelled.clone()))))
        .unwrap();
    assert_eq!(
        pending.take(id).unwrap().consume().unwrap_err().code,
        "boundary_limit"
    );
    assert_eq!(discarded.load(Ordering::SeqCst), 1);
    let (id, _, _) = pending.reserve(4, 4).unwrap();
    pending.cancel(id);
    assert!(
        pending
            .started(id, Ok(Box::new(HostCall(cancelled.clone()))))
            .is_err()
    );
    assert_eq!(cancelled.load(Ordering::SeqCst), 1);
}

#[test]
fn concurrent_completion_and_cancellation_release_every_reply() {
    for _ in 0..64 {
        let mut pending = PendingCalls::new(1, 64, Arc::new(Wake::default()));
        let discarded = Arc::new(AtomicUsize::new(0));
        let cancelled = Arc::new(AtomicUsize::new(0));
        let (id, _, completion) = pending.reserve(8, 16).unwrap();
        pending
            .started(id, Ok(Box::new(HostCall(cancelled.clone()))))
            .unwrap();
        let barrier = Arc::new(std::sync::Barrier::new(2));
        let ready = barrier.clone();
        let observed = discarded.clone();
        let thread = std::thread::spawn(move || {
            ready.wait();
            completion.complete(owned_reply(&observed, vec![1]));
        });
        barrier.wait();
        pending.cancel(id);
        thread.join().unwrap();
        assert_eq!(discarded.load(Ordering::SeqCst), 1);
        assert_eq!(cancelled.load(Ordering::SeqCst), 1);
        assert_eq!(pending.reserved_bytes(), 0);
        assert_eq!(pending.pending_count(), 0);
    }
}
