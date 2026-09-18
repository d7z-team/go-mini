//! Host task and timer boundary. Browser tasks never block the event loop.

#[cfg(not(target_arch = "wasm32"))]
pub use tokio::{
    runtime::Handle,
    time::{sleep, timeout},
};

#[cfg(not(target_arch = "wasm32"))]
pub fn sleep_until(deadline: web_time::Instant) -> tokio::time::Sleep {
    tokio::time::sleep_until(deadline.into())
}

#[cfg(target_arch = "wasm32")]
pub use browser::*;

#[cfg(target_arch = "wasm32")]
mod browser {
    use std::{
        cell::RefCell,
        collections::BTreeMap,
        future::Future,
        pin::Pin,
        task::{Context, Poll},
        time::Duration,
    };
    use tokio::sync::oneshot;
    use wasm_bindgen::{JsCast, prelude::*};

    #[wasm_bindgen(
        inline_js = "export function timerStart(f,ms){return globalThis.setTimeout(f,ms)} export function timerStop(id){globalThis.clearTimeout(id)}"
    )]
    extern "C" {
        fn timerStart(f: &js_sys::Function, ms: f64) -> f64;
        fn timerStop(id: f64);
    }
    struct Timer {
        id: f64,
        _callback: Closure<dyn FnMut()>,
    }
    thread_local! {
        static TIMERS: RefCell<BTreeMap<u64, Timer>> = const { RefCell::new(BTreeMap::new()) };
        static NEXT: std::cell::Cell<u64> = const { std::cell::Cell::new(0) };
    }

    #[derive(Clone, Default)]
    pub struct Handle;
    impl Handle {
        pub fn spawn<F>(&self, future: F) -> oneshot::Receiver<F::Output>
        where
            F: Future + Send + 'static,
            F::Output: Send + 'static,
        {
            let (send, receive) = oneshot::channel();
            wasm_bindgen_futures::spawn_local(async move {
                let _ = send.send(future.await);
            });
            receive
        }
    }

    pub struct Sleep {
        token: u64,
        receive: oneshot::Receiver<()>,
        deadline: web_time::Instant,
    }
    pub fn sleep(duration: Duration) -> Sleep {
        sleep_until(web_time::Instant::now() + duration)
    }
    pub fn sleep_until(deadline: web_time::Instant) -> Sleep {
        let token = NEXT.with(|next| {
            let value = next.get().checked_add(1).expect("timer identity exhausted");
            next.set(value);
            value
        });
        let (send, receive) = oneshot::channel();
        let mut send = Some(send);
        let callback = Closure::wrap(Box::new(move || {
            if let Some(send) = send.take() {
                let _ = send.send(());
            }
        }) as Box<dyn FnMut()>);
        let millis = deadline
            .saturating_duration_since(web_time::Instant::now())
            .as_secs_f64()
            * 1000.0;
        let id = timerStart(
            callback.as_ref().unchecked_ref(),
            millis.ceil().min(2_147_483_647.0),
        );
        TIMERS.with(|timers| {
            timers.borrow_mut().insert(
                token,
                Timer {
                    id,
                    _callback: callback,
                },
            )
        });
        Sleep {
            token,
            receive,
            deadline,
        }
    }
    impl Future for Sleep {
        type Output = ();
        fn poll(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<()> {
            match Pin::new(&mut self.receive).poll(cx) {
                Poll::Pending => Poll::Pending,
                Poll::Ready(_) if web_time::Instant::now() >= self.deadline => Poll::Ready(()),
                Poll::Ready(_) => {
                    *self = sleep_until(self.deadline);
                    self.poll(cx)
                }
            }
        }
    }
    impl Drop for Sleep {
        fn drop(&mut self) {
            TIMERS.with(|timers| {
                if let Some(timer) = timers.borrow_mut().remove(&self.token) {
                    timerStop(timer.id);
                }
            });
        }
    }
    #[derive(Debug)]
    pub struct Elapsed;
    impl std::fmt::Display for Elapsed {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.write_str("deadline elapsed")
        }
    }
    impl std::error::Error for Elapsed {}
    pub async fn timeout<F: Future>(duration: Duration, future: F) -> Result<F::Output, Elapsed> {
        tokio::select! { biased; result = future => Ok(result), _ = sleep(duration) => Err(Elapsed) }
    }
}
