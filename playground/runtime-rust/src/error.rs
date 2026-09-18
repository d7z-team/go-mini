//! Structured failures at the runtime boundary.

use std::fmt;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RuntimeError {
    pub code: &'static str,
    pub path: String,
    pub message: String,
}

impl RuntimeError {
    pub fn new(code: &'static str, path: impl AsRef<str>, message: impl Into<String>) -> Self {
        Self {
            code,
            path: path.as_ref().to_owned(),
            message: message.into(),
        }
    }
}

impl fmt::Display for RuntimeError {
    fn fmt(&self, out: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(out, "{} at {}: {}", self.code, self.path, self.message)
    }
}

impl std::error::Error for RuntimeError {}

impl From<serde_json::Error> for RuntimeError {
    fn from(error: serde_json::Error) -> Self {
        Self::new("invalid_json", "$", error.to_string())
    }
}
