use super::{BlockingPool, console_binding::*};
use crate::rpc::*;
use std::{
    io::{self, Read, Write},
    sync::{Arc, Mutex},
};

/// Implementations may use the context to interrupt an in-progress read.
pub trait Input: Send + Sync {
    fn read(&self, context: &CallContext, buffer: &mut [u8]) -> (usize, Option<InputFault>);
}
pub enum InputFault {
    Eof,
    Io(io::Error),
}
pub struct Reader<R>(pub Mutex<R>);
impl<R: Read + Send> Input for Reader<R> {
    fn read(&self, _: &CallContext, buffer: &mut [u8]) -> (usize, Option<InputFault>) {
        match self.0.lock().unwrap().read(buffer) {
            Ok(0) if !buffer.is_empty() => (0, Some(InputFault::Eof)),
            Ok(count) => (count, None),
            Err(error) => (0, Some(InputFault::Io(error))),
        }
    }
}
pub type Output = Arc<Mutex<dyn Write + Send>>;
pub struct Streams {
    pub input: Option<Arc<dyn Input>>,
    pub stdout: Option<Output>,
    pub stderr: Option<Output>,
    pub pool: Arc<BlockingPool>,
}
impl Streams {
    pub fn provider(self: Arc<Self>) -> Result<Arc<dyn Provider>> {
        fmt_console_provider(Arc::new(ConsoleBinding {
            streams: self,
            read_lock: Arc::new(Mutex::new(())),
        }))
    }
}
struct ConsoleBinding {
    streams: Arc<Streams>,
    read_lock: Arc<Mutex<()>>,
}
impl std::ops::Deref for ConsoleBinding {
    type Target = Streams;
    fn deref(&self) -> &Streams {
        &self.streams
    }
}
impl FmtConsoleHandler for ConsoleBinding {
    fn write(
        &self,
        context: CallContext,
        stream: i64,
        data: Option<Vec<u8>>,
    ) -> BoxFuture<'_, Result<(i64, FmtStreamFault)>> {
        Box::pin(async move {
            context.check()?;
            let output = match stream {
                1 => self.stdout.clone(),
                2 => self.stderr.clone(),
                _ => {
                    return Ok((
                        0,
                        FmtStreamFault {
                            code: "invalid".into(),
                            message: "stream is not writable".into(),
                        },
                    ));
                }
            };
            let data = data.unwrap_or_default();
            let Some(output) = output else {
                return Ok((
                    data.len() as i64,
                    FmtStreamFault {
                        code: String::new(),
                        message: String::new(),
                    },
                ));
            };
            self.pool
                .run(move || match output.lock().unwrap().write(&data) {
                    Ok(count) if count == data.len() => (
                        count as i64,
                        FmtStreamFault {
                            code: String::new(),
                            message: String::new(),
                        },
                    ),
                    Ok(count) => (
                        count as i64,
                        FmtStreamFault {
                            code: "short_write".into(),
                            message: "short write".into(),
                        },
                    ),
                    Err(error) => (
                        0,
                        FmtStreamFault {
                            code: "io".into(),
                            message: error.to_string(),
                        },
                    ),
                })
                .await
        })
    }
    fn read(
        &self,
        context: CallContext,
        size: i64,
    ) -> BoxFuture<'_, Result<(Option<Vec<u8>>, FmtStreamFault)>> {
        Box::pin(async move {
            context.check()?;
            if !(0..=1 << 20).contains(&size) {
                return Ok((
                    None,
                    FmtStreamFault {
                        code: "invalid".into(),
                        message: "read size out of range".into(),
                    },
                ));
            }
            if size == 0 {
                return Ok((
                    Some(Vec::new()),
                    FmtStreamFault {
                        code: String::new(),
                        message: String::new(),
                    },
                ));
            }
            let input = self.input.clone();
            let read_lock = self.read_lock.clone();
            self.pool
                .run(move || {
                    let _read = read_lock.lock().unwrap();
                    context.check()?;
                    let mut buffer = vec![0; size as usize];
                    let (count, fault) = match input {
                        Some(input) => input.read(&context, &mut buffer),
                        None => (0, Some(InputFault::Eof)),
                    };
                    context.check()?;
                    if count > buffer.len() {
                        return Err(Status::new(
                            "internal",
                            "console reader returned invalid count",
                        ));
                    }
                    buffer.truncate(count);
                    Ok(match fault {
                        Some(InputFault::Eof) if count == 0 => (
                            None,
                            FmtStreamFault {
                                code: "eof".into(),
                                message: "EOF".into(),
                            },
                        ),
                        Some(InputFault::Io(error)) => (
                            Some(buffer),
                            FmtStreamFault {
                                code: "io".into(),
                                message: error.to_string(),
                            },
                        ),
                        _ => (
                            Some(buffer),
                            FmtStreamFault {
                                code: String::new(),
                                message: String::new(),
                            },
                        ),
                    })
                })
                .await?
        })
    }
}
