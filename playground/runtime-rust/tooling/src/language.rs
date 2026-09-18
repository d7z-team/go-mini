use crate::session::CompilerSession;
use mini_go::{error::RuntimeError, ffi::Cancellation};
use serde_json::{Value, json};

pub struct LanguageService {
    pub session: CompilerSession,
}
impl LanguageService {
    pub async fn new(image: &[u8]) -> Result<Self, RuntimeError> {
        Ok(Self {
            session: CompilerSession::new(image).await?,
        })
    }
    pub async fn sources(
        &mut self,
        trees: &[crate::sources::SourceTree],
        cancel: &Cancellation,
    ) -> Result<crate::sources::SourcePackages, RuntimeError> {
        let mut reply = self
            .session
            .call(
                json!({"Operation": "workspace/sources", "Trees": trees}),
                cancel,
            )
            .await?;
        serde_json::from_value(reply["Value"].take())
            .map_err(|e| RuntimeError::new("invalid_argument", "sources", e.to_string()))
    }
    pub async fn open(
        &mut self,
        mut workspace: Value,
        cancel: &Cancellation,
    ) -> Result<Value, RuntimeError> {
        workspace["Operation"] = json!("workspace/open");
        self.session.call(workspace, cancel).await
    }
    pub async fn update(
        &mut self,
        changes: Value,
        cancel: &Cancellation,
    ) -> Result<Value, RuntimeError> {
        self.session
            .call(
                json!({"Operation":"document/update","Changes":changes}),
                cancel,
            )
            .await
    }
    pub async fn analyze(&mut self, cancel: &Cancellation) -> Result<Value, RuntimeError> {
        self.session
            .call(json!({"Operation":"workspace/analyze"}), cancel)
            .await
    }
    pub async fn query(
        &mut self,
        operation: &str,
        mut parameters: Value,
        cancel: &Cancellation,
    ) -> Result<Value, RuntimeError> {
        if parameters.get("Snapshot").is_none() {
            parameters["Snapshot"] = json!(self.session.snapshot());
        }
        Ok(self
            .session
            .call(
                json!({"Operation":format!("language/{operation}"),"Query":parameters}),
                cancel,
            )
            .await?["Value"]
            .take())
    }
    pub async fn close(&mut self) -> Result<(), RuntimeError> {
        self.session.close().await
    }
}
