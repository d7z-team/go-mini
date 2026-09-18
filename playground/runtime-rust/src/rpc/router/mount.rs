use super::super::*;
use std::sync::Arc;

/// A provider reached through another Binder; each local lease pins a remote binding.
pub struct MountedProvider {
    binder: Arc<dyn Binder>,
    contract: Contract,
}
impl MountedProvider {
    pub fn new(binder: Arc<dyn Binder>, contract: Contract, limits: &Limits) -> Result<Self> {
        Ok(Self {
            binder,
            contract: contract.normalized(limits)?,
        })
    }
}
impl Provider for MountedProvider {
    fn contract(&self) -> Contract {
        self.contract.clone()
    }
    fn bind(
        &self,
        context: CallContext,
        mut request: BindRequest,
    ) -> BoxFuture<'_, Result<Arc<dyn ProviderLease>>> {
        Box::pin(async move {
            if request.hops <= 0 {
                return Err(Status::exhausted("RPC Router hop limit exceeded"));
            }
            self.contract.check_support(&request.contract)?;
            for method in &self.contract.methods {
                if !method.resource_type_hash.is_empty()
                    && !request.contract.methods.contains(method)
                {
                    request.contract.methods.push(method.clone());
                }
            }
            let routes = self.binder.bind(context, request).await?;
            Ok(Arc::new(MountedLease { routes }) as Arc<dyn ProviderLease>)
        })
    }
}
struct MountedLease {
    routes: Arc<RouteSet>,
}
impl ProviderLease for MountedLease {
    fn invoke(
        &self,
        context: CallContext,
        method: Method,
        arguments: Vec<Value>,
    ) -> BoxFuture<'_, Result<ProviderResult>> {
        Box::pin(forward(&self.routes, context, method, None, arguments))
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(self.routes.shutdown())
    }
}
struct MountedResource {
    routes: Arc<RouteSet>,
    reference: ResourceRef,
}
impl Resource for MountedResource {
    fn invoke(
        &self,
        context: CallContext,
        name: String,
        arguments: Vec<Value>,
    ) -> BoxFuture<'_, Result<Vec<Value>>> {
        Box::pin(async move {
            let method = self
                .routes
                .contract
                .methods
                .iter()
                .find(|m| m.name == name && m.resource_type_hash == self.reference.type_hash)
                .cloned()
                .ok_or_else(|| {
                    Status::new("unimplemented", "mounted resource method unavailable")
                })?;
            let mut result = forward(
                &self.routes,
                context,
                method,
                Some(self.reference.clone()),
                arguments,
            )
            .await?;
            if let Some(decision) = result.decision.take() {
                decision.accept().await?.consume();
            }
            Ok(result.values)
        })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(
            self.routes
                .drop_resource(CallContext::default(), self.reference.clone()),
        )
    }
}
async fn forward(
    routes: &Arc<RouteSet>,
    context: CallContext,
    method: Method,
    receiver: Option<ResourceRef>,
    mut arguments: Vec<Value>,
) -> Result<ProviderResult> {
    for argument in &mut arguments {
        if let Data::Resource(reference) = &argument.data {
            let resource: Arc<dyn std::any::Any + Send + Sync> = context.resolve(reference)?;
            let resource = resource.downcast::<MountedResource>().map_err(|_| {
                Status::new(
                    "invalid_argument",
                    "resource is not from this mounted provider",
                )
            })?;
            if !Arc::ptr_eq(&resource.routes, routes) {
                return Err(Status::new(
                    "invalid_argument",
                    "resource belongs to another mounted binding",
                ));
            }
            *argument = Value::resource(resource.reference.clone());
        }
    }
    let result = routes
        .invoke(
            context.clone(),
            Call {
                method,
                receiver,
                arguments,
            },
        )
        .await?;
    let mut values = result.values.clone();
    for value in &mut values {
        if let Data::Resource(reference) = &value.data {
            *value = context.export(
                Arc::new(MountedResource {
                    routes: routes.clone(),
                    reference: reference.clone(),
                }),
                reference.type_hash.clone(),
            )?;
        }
    }
    Ok(ProviderResult {
        values,
        decision: Some(result),
    })
}
