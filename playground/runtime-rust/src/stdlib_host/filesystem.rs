use super::{BlockingPool, os_binding::*};
use crate::rpc::*;
use std::sync::Arc;

pub type Fault = OsOperationFault;
pub type Metadata = OsFileMetadata;
pub type FsResult<T> = std::result::Result<T, Fault>;
pub fn fault(code: &str) -> Fault {
    Fault {
        code: code.into(),
        message: code.into(),
    }
}
pub fn empty_metadata() -> Metadata {
    Metadata {
        name: String::new(),
        size: 0,
        mode: 0,
        modified_seconds: 0,
        modified_nanoseconds: 0,
        directory: false,
    }
}

pub trait File: Send + Sync {
    fn read(&self, size: i64, offset: Option<i64>) -> (Option<Vec<u8>>, Fault);
    fn write(&self, data: &[u8], offset: Option<i64>) -> (i64, Fault);
    fn seek(&self, offset: i64, whence: i64) -> FsResult<i64>;
    fn stat(&self) -> FsResult<Metadata>;
    fn read_dir(&self, count: i64) -> (Option<Vec<Metadata>>, Fault);
    fn close(&self) -> FsResult<()>;
}
pub trait Filesystem: Send + Sync {
    fn open(&self, name: &str, flags: i64, mode: u32) -> FsResult<Arc<dyn File>>;
    fn stat(&self, name: &str, follow: bool) -> FsResult<Metadata>;
    fn read_dir(&self, name: &str) -> FsResult<Vec<Metadata>>;
    fn mkdir(&self, name: &str, mode: u32) -> FsResult<()>;
    fn remove(&self, name: &str) -> FsResult<()>;
    fn rename(&self, old: &str, new: &str) -> FsResult<()>;
    fn read_link(&self, name: &str) -> FsResult<String>;
    fn chtimes(&self, name: &str, atime: (i64, i64), mtime: (i64, i64)) -> FsResult<()>;
    fn working_directory(&self) -> FsResult<String>;
    fn cache_directory(&self) -> FsResult<String>;
}
pub struct FilesystemProvider {
    pub backend: Arc<dyn Filesystem>,
    pub pool: Arc<BlockingPool>,
}
impl FilesystemProvider {
    pub fn provider(self) -> Result<Arc<dyn Provider>> {
        os_filesystem_provider(Arc::new(self))
    }
}
impl OsFilesystemHandler for FilesystemProvider {
    fn open(
        &self,
        context: CallContext,
        name: String,
        flags: i64,
        mode: u32,
    ) -> BoxFuture<'_, Result<(Option<Arc<dyn OsFileHandleHandler>>, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            let pool = self.pool.clone();
            self.pool.run(move || match backend.open(&name, flags, mode) {
                Ok(file) => (Some(Arc::new(FileBinding::new(file, pool)) as Arc<dyn OsFileHandleHandler>), fault("")),
                Err(error) => (None, error),
            }).await
        })
    }
    fn stat(
        &self,
        context: CallContext,
        name: String,
        follow: bool,
    ) -> BoxFuture<'_, Result<(Metadata, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || match backend.stat(&name, follow) {
                    Ok(value) => (value, fault("")),
                    Err(error) => (empty_metadata(), error),
                })
                .await
        })
    }
    fn read_dir(
        &self,
        context: CallContext,
        name: String,
    ) -> BoxFuture<'_, Result<(Option<Vec<Metadata>>, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || match backend.read_dir(&name) {
                    Ok(value) => (Some(value), fault("")),
                    Err(error) => (None, error),
                })
                .await
        })
    }
    fn mkdir(
        &self,
        context: CallContext,
        name: String,
        mode: u32,
    ) -> BoxFuture<'_, Result<(Fault,)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || {
                    (backend
                        .mkdir(&name, mode)
                        .err()
                        .unwrap_or_else(|| fault("")),)
                })
                .await
        })
    }
    fn remove(&self, context: CallContext, name: String) -> BoxFuture<'_, Result<(Fault,)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || (backend.remove(&name).err().unwrap_or_else(|| fault("")),))
                .await
        })
    }
    fn rename(
        &self,
        context: CallContext,
        old: String,
        new: String,
    ) -> BoxFuture<'_, Result<(Fault,)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || {
                    (backend
                        .rename(&old, &new)
                        .err()
                        .unwrap_or_else(|| fault("")),)
                })
                .await
        })
    }
    fn readlink(
        &self,
        context: CallContext,
        name: String,
    ) -> BoxFuture<'_, Result<(String, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || match backend.read_link(&name) {
                    Ok(value) => (value, fault("")),
                    Err(error) => (String::new(), error),
                })
                .await
        })
    }
    fn chtimes(
        &self,
        context: CallContext,
        name: String,
        a: i64,
        an: i64,
        m: i64,
        mn: i64,
    ) -> BoxFuture<'_, Result<(Fault,)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || {
                    (backend
                        .chtimes(&name, (a, an), (m, mn))
                        .err()
                        .unwrap_or_else(|| fault("")),)
                })
                .await
        })
    }
    fn getwd(&self, context: CallContext) -> BoxFuture<'_, Result<(String, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || match backend.working_directory() {
                    Ok(value) => (value, fault("")),
                    Err(error) => (String::new(), error),
                })
                .await
        })
    }
    fn user_cache_dir(&self, context: CallContext) -> BoxFuture<'_, Result<(String, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let backend = self.backend.clone();
            self.pool
                .run(move || match backend.cache_directory() {
                    Ok(value) => (value, fault("")),
                    Err(error) => (String::new(), error),
                })
                .await
        })
    }
}
struct FileBinding {
    file: Arc<dyn File>,
    pool: Arc<BlockingPool>,
    closing: crate::ffi::Cancellation,
    done: tokio::sync::watch::Receiver<Option<Result<()>>>,
}
impl FileBinding {
    fn new(file: Arc<dyn File>, pool: Arc<BlockingPool>) -> Self {
        let closing = crate::ffi::Cancellation::default();
        let backend = file.clone();
        let done = pool.spawn_cleanup(closing.clone(), move || {
            backend
                .close()
                .map_err(|error| Status::new(error.code, error.message))
        });
        Self {
            file,
            pool,
            closing,
            done,
        }
    }
}
impl Drop for FileBinding {
    fn drop(&mut self) {
        self.closing.cancel();
    }
}
impl OsFileHandleHandler for FileBinding {
    fn read(
        &self,
        context: CallContext,
        size: i64,
    ) -> BoxFuture<'_, Result<(Option<Vec<u8>>, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool
                .run(move || {
                    let (data, mut error) = file.read(size, None);
                    if error.code == "eof" {
                        error = fault("");
                    }
                    (data, error)
                })
                .await
        })
    }
    fn read_at(
        &self,
        context: CallContext,
        size: i64,
        offset: i64,
    ) -> BoxFuture<'_, Result<(Option<Vec<u8>>, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool.run(move || file.read(size, Some(offset))).await
        })
    }
    fn write(
        &self,
        context: CallContext,
        data: Option<Vec<u8>>,
    ) -> BoxFuture<'_, Result<(i64, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool
                .run(move || file.write(&data.unwrap_or_default(), None))
                .await
        })
    }
    fn write_at(
        &self,
        context: CallContext,
        data: Option<Vec<u8>>,
        offset: i64,
    ) -> BoxFuture<'_, Result<(i64, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool
                .run(move || file.write(&data.unwrap_or_default(), Some(offset)))
                .await
        })
    }
    fn seek(
        &self,
        context: CallContext,
        offset: i64,
        whence: i64,
    ) -> BoxFuture<'_, Result<(i64, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool
                .run(move || match file.seek(offset, whence) {
                    Ok(value) => (value, fault("")),
                    Err(error) => (0, error),
                })
                .await
        })
    }
    fn stat(&self, context: CallContext) -> BoxFuture<'_, Result<(Metadata, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool
                .run(move || match file.stat() {
                    Ok(value) => (value, fault("")),
                    Err(error) => (empty_metadata(), error),
                })
                .await
        })
    }
    fn read_dir(
        &self,
        context: CallContext,
        count: i64,
    ) -> BoxFuture<'_, Result<(Option<Vec<Metadata>>, Fault)>> {
        Box::pin(async move {
            context.check()?;
            let file = self.file.clone();
            self.pool.run(move || file.read_dir(count)).await
        })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        self.closing.cancel();
        Box::pin(async move {
            let mut done = self.done.clone();
            let result = done
                .wait_for(Option::is_some)
                .await
                .map_err(|_| Status::new("internal", "file cleanup owner failed"))?;
            result.as_ref().unwrap().clone()
        })
    }
}
