use super::control as wire;
use super::*;
use serde::Serialize;
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};
use std::sync::Arc;
use std::sync::Mutex;

pub const SNAPSHOT_PROTOCOL: &str = "minigo.rpc.gateway.snapshot.v2";

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Snapshot {
    pub id: String,
    pub gateway_id: String,
    pub route_epoch: u64,
    pub references: Vec<ContractReference>,
}
impl Snapshot {
    pub fn new(mut references: Vec<ContractReference>) -> Result<Self> {
        if references.len() > 4096 {
            return Err(Status::exhausted("catalog reference limit exceeded"));
        }
        references.sort();
        references.dedup();
        let mut material = SNAPSHOT_PROTOCOL.as_bytes().to_vec();
        material.push(0);
        for (index, reference) in references.iter().enumerate() {
            if reference.import_path.trim().is_empty()
                || reference.hash.len() != 64
                || !reference
                    .hash
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
            {
                return Err(Status::protocol("invalid catalog reference"));
            }
            if index > 0 && references[index - 1].import_path == reference.import_path {
                return Err(Status::protocol("conflicting catalog references"));
            }
            material.extend(reference.import_path.as_bytes());
            material.push(0);
            material.extend(reference.hash.as_bytes());
            material.push(0);
        }
        Ok(Self {
            id: format!("{:x}", Sha256::digest(material)),
            gateway_id: String::new(),
            route_epoch: 0,
            references,
        })
    }
    fn decode(value: wire::CatalogSnapshot) -> Result<Self> {
        let mut snapshot = Self::new(
            value
                .references
                .unwrap_or_default()
                .iter()
                .map(ContractReference::from)
                .collect(),
        )?;
        if value.protocol != SNAPSHOT_PROTOCOL || value.id != snapshot.id {
            return Err(Status::protocol("catalog snapshot identity mismatch"));
        }
        snapshot.gateway_id = value.gateway_id;
        snapshot.route_epoch = value.route_epoch;
        Ok(snapshot)
    }
    fn into_wire(self) -> wire::CatalogSnapshot {
        wire::CatalogSnapshot {
            protocol: SNAPSHOT_PROTOCOL.into(),
            id: self.id,
            gateway_id: self.gateway_id,
            route_epoch: self.route_epoch,
            references: Some(self.references.iter().map(Into::into).collect()),
        }
    }
}

pub trait CatalogSource: Send + Sync {
    fn snapshot(&self) -> Result<Snapshot>;
    fn watch(&self, context: CallContext, after: Snapshot) -> BoxFuture<'_, Result<Snapshot>>;
    fn resolve(&self, reference: &ContractReference) -> Result<MrpcBundle>;
}
pub fn provider(source: Arc<dyn CatalogSource>) -> Result<Arc<dyn Provider>> {
    wire::catalog_provider(Arc::new(CatalogHandler(source)))
}
struct CatalogHandler(Arc<dyn CatalogSource>);
impl wire::CatalogHandler for CatalogHandler {
    fn snapshot(&self, context: CallContext) -> BoxFuture<'_, Result<(wire::CatalogSnapshot,)>> {
        Box::pin(async move {
            context.check()?;
            Ok((self.0.snapshot()?.into_wire(),))
        })
    }
    fn watch(
        &self,
        context: CallContext,
        gateway_id: String,
        route_epoch: u64,
    ) -> BoxFuture<'_, Result<(wire::CatalogSnapshot,)>> {
        Box::pin(async move {
            Ok((self
                .0
                .watch(
                    context,
                    Snapshot {
                        id: String::new(),
                        gateway_id,
                        route_epoch,
                        references: Vec::new(),
                    },
                )
                .await?
                .into_wire(),))
        })
    }
    fn resolve(
        &self,
        context: CallContext,
        import_path: String,
        hash: String,
    ) -> BoxFuture<'_, Result<(wire::ContractBundle,)>> {
        Box::pin(async move {
            context.check()?;
            Ok((self
                .0
                .resolve(&ContractReference { import_path, hash })?
                .into(),))
        })
    }
}

pub struct Client {
    inner: wire::CatalogClient,
}
impl Client {
    pub async fn bind(context: CallContext, binder: &dyn Binder) -> Result<Self> {
        Ok(Self {
            inner: wire::CatalogClient::bind(context, binder, BindOptions::default()).await?,
        })
    }
    pub async fn snapshot(&self, context: CallContext) -> Result<Snapshot> {
        Snapshot::decode(self.inner.snapshot(context).await?.0)
    }
    pub async fn watch(&self, context: CallContext, after: &Snapshot) -> Result<Snapshot> {
        let snapshot = Snapshot::decode(
            self.inner
                .watch(context, after.gateway_id.clone(), after.route_epoch)
                .await?
                .0,
        )?;
        if snapshot.gateway_id.is_empty()
            || snapshot.gateway_id == after.gateway_id && snapshot.route_epoch == after.route_epoch
        {
            return Err(Status::protocol(
                "catalog Watch returned unchanged revision",
            ));
        }
        Ok(snapshot)
    }
    pub async fn resolve(
        &self,
        context: CallContext,
        reference: &ContractReference,
    ) -> Result<MrpcBundle> {
        let bundle = MrpcBundle::from(
            self.inner
                .resolve(
                    context,
                    reference.import_path.clone(),
                    reference.hash.clone(),
                )
                .await?
                .0,
        );
        bundle.validate()?;
        if bundle.reference() != *reference {
            return Err(Status::protocol("catalog resolved another bundle"));
        }
        Ok(bundle)
    }
    pub async fn close(&self) -> Result<()> {
        self.inner.close().await
    }
}

#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Serialize)]
pub struct ContractReference {
    pub import_path: String,
    pub hash: String,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct MrpcFile {
    pub path: String,
    pub text: String,
    pub hash: String,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize)]
pub struct MrpcBundle {
    pub import_path: String,
    pub hash: String,
    pub files: Vec<MrpcFile>,
}
impl MrpcBundle {
    pub fn new(import_path: String, files: Vec<MrpcFile>) -> Result<Self> {
        let mut bundle = Self {
            import_path,
            hash: String::new(),
            files,
        };
        bundle.normalize(true)?;
        Ok(bundle)
    }
    pub fn validate(&self) -> Result<()> {
        let mut bundle = self.clone();
        bundle.normalize(false)
    }
    fn normalize(&mut self, compute: bool) -> Result<()> {
        let valid_path = |path: &str| {
            !path.is_empty()
                && path.trim() == path
                && !path.starts_with('/')
                && !path
                    .split('/')
                    .any(|part| part.is_empty() || part == "." || part == "..")
        };
        if !valid_path(&self.import_path) || self.files.is_empty() {
            return Err(Status::protocol("invalid MRPC bundle path or files"));
        }
        let mut paths = BTreeSet::new();
        let mut bytes = self.import_path.len();
        for file in &mut self.files {
            if !valid_path(&file.path)
                || !file.path.ends_with(".mrpc")
                || !paths.insert(file.path.clone())
            {
                return Err(Status::protocol("invalid or duplicate MRPC file path"));
            }
            bytes = bytes
                .saturating_add(file.path.len())
                .saturating_add(file.text.len())
                .saturating_add(64);
            if bytes > 4 << 20 {
                return Err(Status::exhausted("MRPC bundle size limit exceeded"));
            }
            let hash = format!("{:x}", Sha256::digest(file.text.as_bytes()));
            if !compute && file.hash != hash {
                return Err(Status::protocol("MRPC file hash mismatch"));
            }
            file.hash = hash;
        }
        self.files.sort_by(|a, b| a.path.cmp(&b.path));
        #[derive(Serialize)]
        struct Material<'a> {
            import_path: &'a str,
            files: &'a [MrpcFile],
        }
        let json = go_json(&Material {
            import_path: &self.import_path,
            files: &self.files,
        })?;
        let mut material = b"minigo.mrpc.bundle.v1\0".to_vec();
        material.extend(json);
        let hash = format!("{:x}", Sha256::digest(material));
        if !compute && self.hash != hash {
            return Err(Status::protocol("MRPC bundle hash mismatch"));
        }
        self.hash = hash;
        Ok(())
    }
    pub fn reference(&self) -> ContractReference {
        ContractReference {
            import_path: self.import_path.clone(),
            hash: self.hash.clone(),
        }
    }
}

/// Immutable content-addressed source bundles, bounded independently of VM images.
pub struct ContractRepository {
    state: Mutex<RepositoryState>,
    max_bytes: usize,
}
#[derive(Default)]
struct RepositoryState {
    bundles: BTreeMap<ContractReference, MrpcBundle>,
    bytes: usize,
}
impl Default for ContractRepository {
    fn default() -> Self {
        Self::new(64 << 20)
    }
}
impl ContractRepository {
    pub fn new(max_bytes: usize) -> Self {
        Self {
            state: Mutex::new(RepositoryState::default()),
            max_bytes,
        }
    }
    pub fn publish(&self, values: Vec<MrpcBundle>) -> Result<()> {
        let mut state = self.state.lock().unwrap();
        let (additions, bytes) = self.prepare(&state, &values)?;
        let additions = additions
            .into_iter()
            .map(|(key, value)| (key, value.clone()))
            .collect::<BTreeMap<_, _>>();
        state.bundles.extend(additions);
        state.bytes = bytes;
        Ok(())
    }
    pub fn check_capacity(&self, values: &[MrpcBundle]) -> Result<()> {
        self.prepare(&self.state.lock().unwrap(), values)
            .map(|_| ())
    }
    fn prepare<'a>(
        &self,
        state: &RepositoryState,
        values: &'a [MrpcBundle],
    ) -> Result<(BTreeMap<ContractReference, &'a MrpcBundle>, usize)> {
        for bundle in values {
            bundle.validate()?;
        }
        let additions: BTreeMap<_, _> = values
            .iter()
            .filter(|bundle| !state.bundles.contains_key(&bundle.reference()))
            .map(|bundle| (bundle.reference(), bundle))
            .collect();
        let bytes = additions.values().fold(state.bytes, |bytes, bundle| {
            bundle.files.iter().fold(
                bytes.saturating_add(2 * (bundle.import_path.len() + bundle.hash.len())),
                |bytes, file| {
                    bytes
                        .saturating_add(file.path.len())
                        .saturating_add(file.text.len())
                        .saturating_add(file.hash.len())
                },
            )
        });
        if bytes > self.max_bytes {
            return Err(Status::exhausted("MRPC repository limit exceeded"));
        }
        Ok((additions, bytes))
    }
    pub fn resolve(&self, reference: &ContractReference) -> Result<MrpcBundle> {
        self.state
            .lock()
            .unwrap()
            .bundles
            .get(reference)
            .cloned()
            .ok_or_else(|| Status::new("not_found", "MRPC source bundle not found"))
    }
    pub fn references(&self) -> Vec<ContractReference> {
        self.state.lock().unwrap().bundles.keys().cloned().collect()
    }
}

pub(crate) fn go_json(value: &impl Serialize) -> Result<Vec<u8>> {
    let text =
        serde_json::to_string(value).map_err(|error| Status::new("internal", error.to_string()))?;
    Ok(text
        .replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
        .into_bytes())
}
