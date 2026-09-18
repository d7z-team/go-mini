use mini_go_tooling::{lsp::LanguageServer, server::Server};

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut args = std::env::args().skip(1);
    let mode = args
        .next()
        .ok_or("usage: mini-go-tools lsp COMPILER_IMAGE WORKSPACE_JSON | dap")?;
    let server = match mode.as_str() {
        "lsp" => {
            let image = tokio::fs::read(args.next().ok_or("missing compiler image")?).await?;
            let workspace =
                tokio::fs::read(args.next().ok_or("missing prepared workspace JSON")?).await?;
            Server::Language(Box::new(
                LanguageServer::new(&image, serde_json::from_slice(&workspace)?).await?,
            ))
        }
        "dap" => Server::Debug(Box::default()),
        _ => return Err("expected lsp or dap".into()),
    };
    if args.next().is_some() {
        return Err("unexpected arguments".into());
    }
    server
        .serve(tokio::io::stdin(), tokio::io::stdout())
        .await?;
    Ok(())
}
