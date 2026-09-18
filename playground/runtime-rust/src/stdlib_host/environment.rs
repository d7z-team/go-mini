use super::os_binding::*;
use crate::rpc::*;
use std::{collections::BTreeMap, sync::Arc};

pub struct Environment {
    values: BTreeMap<String, String>,
}
impl Environment {
    pub fn snapshot(entries: impl IntoIterator<Item = String>) -> Self {
        Self {
            values: entries
                .into_iter()
                .filter_map(|entry| {
                    entry
                        .split_once('=')
                        .map(|(key, value)| (key.to_owned(), value.to_owned()))
                })
                .collect(),
        }
    }
    pub fn provider(self) -> Result<Arc<dyn Provider>> {
        os_environment_provider(Arc::new(self))
    }
}
impl OsEnvironmentHandler for Environment {
    fn lookup(&self, context: CallContext, key: String) -> BoxFuture<'_, Result<(String, bool)>> {
        Box::pin(async move {
            context.check()?;
            Ok(self
                .values
                .get(&key)
                .map(|value| (value.clone(), true))
                .unwrap_or_default())
        })
    }
}
