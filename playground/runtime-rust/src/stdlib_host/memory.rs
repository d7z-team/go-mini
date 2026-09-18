//! Deterministic writable filesystem with Go slash-relative path semantics.
use super::filesystem::*;
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
};

const DIRECTORY: u32 = 1 << 31;
struct Node {
    data: Vec<u8>,
    mode: u32,
    modified: (i64, i64),
}
impl Node {
    fn metadata(&self, name: &str) -> Metadata {
        Metadata {
            name: name.rsplit('/').next().unwrap_or(name).into(),
            size: self.data.len() as i64,
            mode: self.mode,
            modified_seconds: self.modified.0,
            modified_nanoseconds: self.modified.1,
            directory: self.mode & DIRECTORY != 0,
        }
    }
}
struct State {
    nodes: BTreeMap<String, Node>,
    bytes: usize,
}
pub struct MemoryFilesystem {
    state: Arc<Mutex<State>>,
    max_bytes: usize,
    max_entries: usize,
}
fn clean_path(name: &str) -> FsResult<String> {
    if name.starts_with('/') {
        return Err(fault("invalid"));
    }
    let mut parts = Vec::new();
    for part in name.split('/') {
        match part {
            "" | "." => {}
            ".." => {
                if parts.pop().is_none() {
                    return Err(fault("invalid"));
                }
            }
            value => parts.push(value),
        }
    }
    Ok(if parts.is_empty() {
        ".".into()
    } else {
        parts.join("/")
    })
}
fn parent(name: &str) -> &str {
    name.rsplit_once('/').map_or(".", |(parent, _)| parent)
}
impl MemoryFilesystem {
    pub fn new(files: BTreeMap<String, Vec<u8>>) -> FsResult<Arc<Self>> {
        Self::with_limits(files, 64 << 20, 4096)
    }
    pub fn with_limits(
        files: BTreeMap<String, Vec<u8>>,
        max_bytes: usize,
        max_entries: usize,
    ) -> FsResult<Arc<Self>> {
        let mut nodes = BTreeMap::from([(
            ".".into(),
            Node {
                data: Vec::new(),
                mode: DIRECTORY | 0o777,
                modified: (0, 0),
            },
        )]);
        let mut bytes = 0usize;
        for (name, data) in files {
            let name = clean_path(&name)?;
            if name == "." {
                return Err(fault("invalid"));
            }
            let mut directory = parent(&name);
            while directory != "." {
                if nodes
                    .get(directory)
                    .is_some_and(|node| node.mode & DIRECTORY == 0)
                {
                    return Err(fault("invalid"));
                }
                nodes.entry(directory.into()).or_insert(Node {
                    data: Vec::new(),
                    mode: DIRECTORY | 0o777,
                    modified: (0, 0),
                });
                directory = parent(directory);
            }
            if nodes.contains_key(&name) {
                return Err(fault("exist"));
            }
            bytes = bytes
                .checked_add(data.len())
                .ok_or_else(|| fault("invalid"))?;
            nodes.insert(
                name,
                Node {
                    data,
                    mode: 0o666,
                    modified: (0, 0),
                },
            );
        }
        if bytes > max_bytes || nodes.len() > max_entries {
            return Err(fault("io"));
        }
        Ok(Arc::new(Self {
            state: Arc::new(Mutex::new(State { nodes, bytes })),
            max_bytes,
            max_entries,
        }))
    }
}
impl Filesystem for MemoryFilesystem {
    fn open(&self, name: &str, flags: i64, mode: u32) -> FsResult<Arc<dyn File>> {
        let name = clean_path(name)?;
        let mut state = self.state.lock().unwrap();
        if !state.nodes.contains_key(&name) {
            if flags & 64 == 0
                || !state
                    .nodes
                    .get(parent(&name))
                    .is_some_and(|node| node.mode & DIRECTORY != 0)
            {
                return Err(fault("not_exist"));
            }
            if state.nodes.len() >= self.max_entries {
                return Err(fault("io"));
            }
            state.nodes.insert(
                name.clone(),
                Node {
                    data: Vec::new(),
                    mode: mode & 0o777,
                    modified: (0, 0),
                },
            );
        } else if flags & 64 != 0 && flags & 128 != 0 {
            return Err(fault("exist"));
        }
        let node = state.nodes.get_mut(&name).unwrap();
        if node.mode & DIRECTORY != 0 && flags & 3 != 0 {
            return Err(fault("permission"));
        }
        let removed = if flags & 512 != 0 && flags & 3 != 0 && node.mode & DIRECTORY == 0 {
            let count = node.data.len();
            node.data.clear();
            count
        } else {
            0
        };
        let offset = if flags & 1024 != 0 {
            node.data.len() as i64
        } else {
            0
        };
        state.bytes -= removed;
        Ok(Arc::new(MemoryFile {
            filesystem: self.state.clone(),
            name,
            flags,
            max_bytes: self.max_bytes,
            cursor: Mutex::new(Cursor {
                offset,
                directory: 0,
                closed: false,
            }),
        }))
    }
    fn stat(&self, name: &str, _: bool) -> FsResult<Metadata> {
        let name = clean_path(name)?;
        self.state
            .lock()
            .unwrap()
            .nodes
            .get(&name)
            .map(|node| node.metadata(&name))
            .ok_or_else(|| fault("not_exist"))
    }
    fn read_dir(&self, name: &str) -> FsResult<Vec<Metadata>> {
        let name = clean_path(name)?;
        directory_entries(&self.state.lock().unwrap(), &name)
    }
    fn mkdir(&self, name: &str, mode: u32) -> FsResult<()> {
        let name = clean_path(name)?;
        if name == "." {
            return Err(fault("invalid"));
        }
        let mut state = self.state.lock().unwrap();
        if state.nodes.contains_key(&name) {
            return Err(fault("exist"));
        }
        if !state
            .nodes
            .get(parent(&name))
            .is_some_and(|node| node.mode & DIRECTORY != 0)
        {
            return Err(fault("not_exist"));
        }
        if state.nodes.len() >= self.max_entries {
            return Err(fault("io"));
        }
        state.nodes.insert(
            name,
            Node {
                data: Vec::new(),
                mode: DIRECTORY | (mode & 0o777),
                modified: (0, 0),
            },
        );
        Ok(())
    }
    fn remove(&self, name: &str) -> FsResult<()> {
        let name = clean_path(name)?;
        if name == "." {
            return Err(fault("invalid"));
        }
        let mut state = self.state.lock().unwrap();
        if !state.nodes.contains_key(&name) {
            return Err(fault("not_exist"));
        }
        let prefix = format!("{name}/");
        if state.nodes.keys().any(|key| key.starts_with(&prefix)) {
            return Err(fault("io"));
        }
        state.bytes -= state.nodes.remove(&name).unwrap().data.len();
        Ok(())
    }
    fn rename(&self, old: &str, new: &str) -> FsResult<()> {
        let old = clean_path(old)?;
        let new = clean_path(new)?;
        if old == "." || new == "." || new.starts_with(&format!("{old}/")) {
            return Err(fault("invalid"));
        }
        let mut state = self.state.lock().unwrap();
        if !state.nodes.contains_key(&old)
            || !state
                .nodes
                .get(parent(&new))
                .is_some_and(|node| node.mode & DIRECTORY != 0)
        {
            return Err(fault("not_exist"));
        }
        if state.nodes.contains_key(&new) {
            return Err(fault("exist"));
        }
        let prefix = format!("{old}/");
        let keys = state
            .nodes
            .keys()
            .filter(|key| **key == old || key.starts_with(&prefix))
            .cloned()
            .collect::<Vec<_>>();
        for key in keys {
            let node = state.nodes.remove(&key).unwrap();
            state
                .nodes
                .insert(format!("{new}{}", &key[old.len()..]), node);
        }
        Ok(())
    }
    fn read_link(&self, _: &str) -> FsResult<String> {
        Err(fault("invalid"))
    }
    fn chtimes(&self, name: &str, _: (i64, i64), mtime: (i64, i64)) -> FsResult<()> {
        let name = clean_path(name)?;
        let mut state = self.state.lock().unwrap();
        let node = state
            .nodes
            .get_mut(&name)
            .ok_or_else(|| fault("not_exist"))?;
        node.modified = (
            mtime
                .0
                .checked_add(mtime.1.div_euclid(1_000_000_000))
                .ok_or_else(|| fault("invalid"))?,
            mtime.1.rem_euclid(1_000_000_000),
        );
        Ok(())
    }
    fn working_directory(&self) -> FsResult<String> {
        Ok(".".into())
    }
    fn cache_directory(&self) -> FsResult<String> {
        Ok(".cache".into())
    }
}
fn directory_entries(state: &State, name: &str) -> FsResult<Vec<Metadata>> {
    if !state
        .nodes
        .get(name)
        .is_some_and(|node| node.mode & DIRECTORY != 0)
    {
        return Err(fault("not_exist"));
    }
    Ok(state
        .nodes
        .iter()
        .filter(|(key, _)| key.as_str() != "." && parent(key) == name)
        .map(|(key, node)| node.metadata(key))
        .collect())
}
struct Cursor {
    offset: i64,
    directory: usize,
    closed: bool,
}
struct MemoryFile {
    filesystem: Arc<Mutex<State>>,
    name: String,
    flags: i64,
    max_bytes: usize,
    cursor: Mutex<Cursor>,
}
impl File for MemoryFile {
    fn read(&self, size: i64, offset: Option<i64>) -> (Option<Vec<u8>>, Fault) {
        let mut cursor = self.cursor.lock().unwrap();
        if cursor.closed {
            return (None, fault("closed"));
        }
        let position = offset.unwrap_or(cursor.offset);
        if !(0..=64 << 20).contains(&size) || position < 0 {
            return (None, fault("invalid"));
        }
        let state = self.filesystem.lock().unwrap();
        let Some(node) = state.nodes.get(&self.name) else {
            return (Some(Vec::new()), fault("not_exist"));
        };
        if node.mode & DIRECTORY != 0 || self.flags & 3 == 1 {
            return (Some(Vec::new()), fault("permission"));
        }
        let start = usize::try_from(position)
            .unwrap_or(usize::MAX)
            .min(node.data.len());
        let end = start.saturating_add(size as usize).min(node.data.len());
        let data = node.data[start..end].to_vec();
        let incomplete = data.len() < size as usize;
        if offset.is_none() {
            cursor.offset += data.len() as i64;
        }
        let code = if incomplete && (offset.is_some() || data.is_empty()) {
            "eof"
        } else {
            ""
        };
        (Some(data), fault(code))
    }
    fn write(&self, data: &[u8], offset: Option<i64>) -> (i64, Fault) {
        let mut cursor = self.cursor.lock().unwrap();
        if offset.is_some() && self.flags & 1024 != 0 {
            return (0, fault("io"));
        }
        if cursor.closed {
            return (0, fault("closed"));
        }
        let mut state = self.filesystem.lock().unwrap();
        let Some(node) = state.nodes.get(&self.name) else {
            return (0, fault("not_exist"));
        };
        if node.mode & DIRECTORY != 0 || self.flags & 3 == 0 {
            return (0, fault("permission"));
        }
        let position = offset.unwrap_or(if self.flags & 1024 != 0 {
            node.data.len() as i64
        } else {
            cursor.offset
        });
        let Ok(position) = usize::try_from(position) else {
            return (0, fault("invalid"));
        };
        let Some(end) = position
            .checked_add(data.len())
            .filter(|end| *end <= i64::MAX as usize)
        else {
            return (0, fault("invalid"));
        };
        if data.is_empty() {
            return (0, fault(""));
        }
        let extra = end.saturating_sub(node.data.len());
        if state.bytes.saturating_add(extra) > self.max_bytes {
            return (0, fault("io"));
        }
        state.bytes += extra;
        let node = state.nodes.get_mut(&self.name).unwrap();
        if end > node.data.len() {
            node.data.resize(end, 0);
        }
        node.data[position..end].copy_from_slice(data);
        if offset.is_none() {
            cursor.offset = end as i64;
        }
        (data.len() as i64, fault(""))
    }
    fn seek(&self, offset: i64, whence: i64) -> FsResult<i64> {
        let mut cursor = self.cursor.lock().unwrap();
        if cursor.closed {
            return Err(fault("closed"));
        }
        let state = self.filesystem.lock().unwrap();
        let node = state
            .nodes
            .get(&self.name)
            .ok_or_else(|| fault("invalid"))?;
        if node.mode & DIRECTORY != 0 {
            return Err(fault("invalid"));
        }
        let base = match whence {
            0 => 0,
            1 => cursor.offset,
            2 => node.data.len() as i64,
            _ => return Err(fault("invalid")),
        };
        let position = base
            .checked_add(offset)
            .filter(|value| *value >= 0)
            .ok_or_else(|| fault("invalid"))?;
        cursor.offset = position;
        Ok(position)
    }
    fn stat(&self) -> FsResult<Metadata> {
        let cursor = self.cursor.lock().unwrap();
        if cursor.closed {
            return Err(fault("closed"));
        }
        self.filesystem
            .lock()
            .unwrap()
            .nodes
            .get(&self.name)
            .map(|node| node.metadata(&self.name))
            .ok_or_else(|| fault("not_exist"))
    }
    fn read_dir(&self, count: i64) -> (Option<Vec<Metadata>>, Fault) {
        let mut cursor = self.cursor.lock().unwrap();
        if cursor.closed {
            return (None, fault("closed"));
        }
        let state = self.filesystem.lock().unwrap();
        if !state
            .nodes
            .get(&self.name)
            .is_some_and(|node| node.mode & DIRECTORY != 0)
        {
            return (None, fault("invalid"));
        }
        let entries = directory_entries(&state, &self.name).unwrap();
        if cursor.directory >= entries.len() {
            return (Some(Vec::new()), fault(if count > 0 { "eof" } else { "" }));
        }
        let end = if count <= 0 {
            entries.len()
        } else {
            cursor
                .directory
                .saturating_add(count as usize)
                .min(entries.len())
        };
        let values = entries[cursor.directory..end].to_vec();
        cursor.directory = end;
        (Some(values), fault(""))
    }
    fn close(&self) -> FsResult<()> {
        let mut cursor = self.cursor.lock().unwrap();
        if cursor.closed {
            return Err(fault("closed"));
        }
        cursor.closed = true;
        Ok(())
    }
}
