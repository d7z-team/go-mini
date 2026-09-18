use super::*;

impl EndpointState {
    pub(super) async fn maintain_leases(weak: std::sync::Weak<Self>) {
        let mut last_acknowledged = Instant::now();
        'maintenance: loop {
            let Some(state) = weak.upgrade() else {
                break;
            };
            if state.ready(CallContext::default()).await.is_err() {
                break;
            }
            let health_window = state
                .options
                .lease_ttl
                .min(state.state.lock().unwrap().peer_lease_ttl);
            let maintenance_interval = health_window / 4;
            tokio::select! { _ = state.stopping.cancelled() => break, _ = crate::rpc::platform::sleep(maintenance_interval) => {} }
            state.expire_leases();
            let mut covered = 0;
            loop {
                let (bindings, operations, total) = state.renewal_targets();
                if bindings.is_empty() && operations.is_empty() && covered != 0 {
                    break;
                }
                let values = encode_lease_targets(&bindings, &operations);
                let sent_at = Instant::now();
                let context = CallContext {
                    deadline: Some(sent_at + state.options.admission_timeout),
                    ..CallContext::default()
                };
                let response = state
                    .request(
                        context,
                        Frame {
                            kind: RENEW,
                            values,
                            ..Frame::default()
                        },
                    )
                    .await;
                let response = match response {
                    Ok(response) => response,
                    Err(_) => {
                        if state.stopping.is_cancelled() {
                            return;
                        }
                        if last_acknowledged.elapsed() >= health_window {
                            state.fail(Status::new(
                                "unavailable",
                                "RPC renewal health window expired",
                            ));
                            return;
                        }
                        continue 'maintenance;
                    }
                };
                last_acknowledged = Instant::now();
                if let Err(error) = state.apply_renewal_ack(
                    &bindings,
                    &operations,
                    sent_at,
                    response.lease_ttl,
                    &response.values,
                ) {
                    state.fail(error);
                    return;
                }
                covered += bindings.len() + operations.len();
                if covered >= total {
                    break;
                }
            }
        }
    }
    fn renewal_targets(&self) -> (Vec<u64>, Vec<u64>, usize) {
        let now = Instant::now();
        let mut state = self.state.lock().unwrap();
        let mut all = state
            .outbound_binding_leases
            .iter()
            .filter_map(|(id, expires)| (*expires > now).then_some(*id))
            .map(|id| (LEASE_BINDING, id))
            .collect::<Vec<_>>();
        all.extend(
            state
                .outbound_operations
                .iter()
                .filter_map(|(id, (expires, _))| (*expires > now).then_some(*id))
                .map(|id| (LEASE_OPERATION, id)),
        );
        if all.is_empty() {
            return (Vec::new(), Vec::new(), 0);
        }
        let all_len = all.len();
        let start = state.renew_cursor % all_len;
        all.rotate_left(start);
        // The encoded prefix grows monotonically. Probe the actual envelope
        // with binary search instead of rebuilding every shorter prefix.
        let mut count = 0;
        let mut upper = all_len.min(state.outbound_limits.max_pending_controls);
        let mut bindings = Vec::new();
        let mut operations = Vec::new();
        let mut probe = Frame {
            kind: RENEW_ACK,
            origin: self.origin.clone(),
            target_id: u64::MAX,
            reply: true,
            lease_ttl: i64::MAX,
            ..Frame::default()
        };
        while count < upper {
            let candidate = count + (upper - count).div_ceil(2);
            bindings.clear();
            operations.clear();
            for &(kind, id) in &all[..candidate] {
                if kind == LEASE_BINDING {
                    bindings.push(id);
                } else {
                    operations.push(id);
                }
            }
            probe.values = encode_lease_targets(&bindings, &operations);
            if probe
                .encode(state.outbound_limits.max_message_bytes)
                .is_ok_and(|encoded| encoded.len() + 35 <= state.outbound_limits.max_frame_bytes)
            {
                count = candidate;
            } else {
                upper = candidate - 1;
            }
        }
        state.renew_cursor = (start + count) % all_len;
        bindings.clear();
        operations.clear();
        for &(kind, id) in &all[..count] {
            if kind == LEASE_BINDING {
                bindings.push(id);
            } else {
                operations.push(id);
            }
        }
        (bindings, operations, all_len)
    }
    pub(super) fn apply_renewal_ack(
        &self,
        expected_bindings: &[u64],
        expected_operations: &[u64],
        sent_at: Instant,
        granted_nanos: i64,
        payload: &[u8],
    ) -> Result<()> {
        let (bindings, operations) = decode_lease_targets(payload, &self.options.limits)?;
        let allowed_bindings: std::collections::BTreeSet<_> =
            expected_bindings.iter().copied().collect();
        let allowed_operations: std::collections::BTreeSet<_> =
            expected_operations.iter().copied().collect();
        if bindings.iter().any(|id| !allowed_bindings.contains(id))
            || operations.iter().any(|id| !allowed_operations.contains(id))
        {
            return Err(Status::protocol(
                "RPC renewal acknowledgement contains an unexpected target",
            ));
        }
        let grant = if granted_nanos > 0 {
            Duration::from_nanos(granted_nanos as u64)
        } else {
            return Err(Status::protocol("RPC renewal grant must be positive"));
        };
        let expires = sent_at + grant;
        let now = Instant::now();
        let mut state = self.state.lock().unwrap();
        if expires <= now {
            return Ok(());
        }
        for id in bindings {
            if let Some(current) = state.outbound_binding_leases.get_mut(&id)
                && *current > now
            {
                *current = (*current).max(expires);
            }
        }
        for id in operations {
            if let Some((current, _)) = state.outbound_operations.get_mut(&id)
                && *current > now
            {
                *current = (*current).max(expires);
            }
        }
        Ok(())
    }
    pub(super) fn renew_inbound_targets(&self, payload: &[u8]) -> Result<Vec<u8>> {
        let (bindings, operations) = decode_lease_targets(payload, &self.options.limits)?;
        let now = Instant::now();
        let expires = now + self.options.lease_ttl;
        let mut acknowledged_bindings = Vec::new();
        let mut acknowledged_operations = Vec::new();
        let mut state = self.state.lock().unwrap();
        for id in bindings {
            if let Some(current) = state.binding_leases.get_mut(&id)
                && *current > now
            {
                *current = expires;
                acknowledged_bindings.push(id);
            }
        }
        for id in operations {
            if let Some(current) = state.active_leases.get_mut(&id)
                && *current > now
            {
                *current = expires;
                acknowledged_operations.push(id);
                continue;
            }
            if let Some(current) = state
                .results
                .get_mut(&id)
                .map(|entry| &mut entry.expires_at)
                && *current > now
            {
                *current = expires;
                acknowledged_operations.push(id);
            }
        }
        Ok(encode_lease_targets(
            &acknowledged_bindings,
            &acknowledged_operations,
        ))
    }
    fn expire_leases(self: &Arc<Self>) {
        let now = Instant::now();
        let (inbound, outbound, results, outbound_operations, active) = {
            let mut state = self.state.lock().unwrap();
            let inbound_ids: Vec<_> = state
                .binding_leases
                .iter()
                .filter_map(|(id, expires)| (*expires <= now).then_some(*id))
                .collect();
            let mut expired_bindings = Vec::new();
            let inbound = inbound_ids
                .into_iter()
                .filter_map(|id| {
                    state.binding_leases.remove(&id)?;
                    expired_bindings.push(id);
                    state.bindings.remove(&id)
                })
                .collect::<Vec<_>>();
            let expired_active_ids: Vec<_> = state
                .active_bindings
                .iter()
                .filter_map(|(id, binding)| expired_bindings.contains(binding).then_some(*id))
                .collect();
            let mut binding_active = Vec::new();
            for id in expired_active_ids {
                state.active_leases.remove(&id);
                state.active_bindings.remove(&id);
                if let Some(cancellation) = state.active.get(&id).cloned() {
                    binding_active.push(cancellation);
                }
            }
            let outbound_ids: Vec<_> = state
                .outbound_binding_leases
                .iter()
                .filter_map(|(id, expires)| (*expires <= now).then_some(*id))
                .collect();
            let outbound = outbound_ids
                .into_iter()
                .filter_map(|id| {
                    state.outbound_binding_leases.remove(&id)?;
                    state.outbound_bindings.remove(&id)
                })
                .collect::<Vec<_>>();
            let result_ids: Vec<_> = state
                .results
                .iter()
                .filter_map(|(id, entry)| {
                    (entry.expires_at <= now || expired_bindings.contains(&entry.binding))
                        .then_some(*id)
                })
                .collect();
            let results = result_ids
                .into_iter()
                .filter_map(|id| state.results.remove(&id))
                .collect::<Vec<_>>();
            let active_ids: Vec<_> = state
                .active_leases
                .iter()
                .filter_map(|(id, expires)| (*expires <= now).then_some(*id))
                .collect();
            let mut active = active_ids
                .into_iter()
                .filter_map(|id| {
                    state.active_leases.remove(&id)?;
                    state.active_bindings.remove(&id);
                    state.active.get(&id).cloned()
                })
                .collect::<Vec<_>>();
            let outbound_operation_ids: Vec<_> = state
                .outbound_operations
                .iter()
                .filter_map(|(id, (expires, _))| (*expires <= now).then_some(*id))
                .collect();
            let outbound_operations = outbound_operation_ids
                .into_iter()
                .filter_map(|id| {
                    state
                        .outbound_operations
                        .remove(&id)
                        .map(|(_, cancel)| cancel)
                })
                .collect::<Vec<_>>();
            active.extend(binding_active);
            (inbound, outbound, results, outbound_operations, active)
        };
        for entry in results {
            let owner = self.clone();
            self.runtime.spawn(async move {
                owner.record_cleanup(entry.result.discard().await);
                drop(entry._permit);
            });
        }
        for cancellation in outbound_operations {
            cancellation.cancel();
        }
        for cancellation in active {
            cancellation.cancel();
        }
        for entry in inbound {
            entry.routes.binding.begin_shutdown();
            let owner = self.clone();
            self.runtime.spawn(async move {
                owner.record_cleanup(entry.routes.shutdown().await);
                drop(entry._permit);
            });
        }
        for routes in outbound {
            routes.binding.begin_shutdown();
            let owner = self.clone();
            self.runtime.spawn(async move {
                owner.record_cleanup(routes.shutdown().await);
            });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct IdleConnection(Cancellation);
    impl MessageConn for IdleConnection {
        fn read(&self) -> BoxFuture<'_, Result<Vec<u8>>> {
            Box::pin(async {
                self.0.cancelled().await;
                Err(Status::new("unavailable", "closed"))
            })
        }
        fn write(&self, _: Vec<u8>) -> BoxFuture<'_, Result<()>> {
            Box::pin(async { Ok(()) })
        }
        fn close(&self) {
            self.0.cancel();
        }
    }

    #[tokio::test(flavor = "current_thread")]
    async fn renewal_batches_fit_wire_limits_and_cover_live_owners() {
        for frame_bytes in [256, 1024, 1 << 20] {
            let endpoint = Endpoint::open(
                Handle::current(),
                Arc::new(IdleConnection(Cancellation::default())),
                None,
                EndpointOptions::default(),
            )
            .unwrap();
            let expires = Instant::now() + Duration::from_secs(3600);
            let mut expected = Vec::new();
            {
                let mut state = endpoint.state.state.lock().unwrap();
                state.outbound_limits.max_frame_bytes = frame_bytes;
                state.outbound_limits.max_pending_controls = 128;
                for id in 1..=140 {
                    state.outbound_binding_leases.insert(id, expires);
                    state
                        .outbound_operations
                        .insert(u64::MAX - id, (expires, Cancellation::default()));
                }
                state
                    .outbound_binding_leases
                    .insert(0, Instant::now() - Duration::from_secs(1));
                expected.extend((1..=140).map(|id| (LEASE_BINDING, id)));
                expected.extend((1..=140).rev().map(|id| (LEASE_OPERATION, u64::MAX - id)));
            }
            let mut covered = std::collections::BTreeSet::new();
            for _ in 0..expected.len() {
                let start = endpoint.state.state.lock().unwrap().renew_cursor;
                let (bindings, operations, total) = endpoint.state.renewal_targets();
                assert_eq!(total, expected.len());
                let mut probe = Frame {
                    kind: RENEW_ACK,
                    origin: endpoint.state.origin.clone(),
                    target_id: u64::MAX,
                    reply: true,
                    lease_ttl: i64::MAX,
                    ..Frame::default()
                };
                let mut expected_bindings = Vec::new();
                let mut expected_operations = Vec::new();
                // Independent linear oracle checks the largest fitting prefix,
                // including count and ID varint boundaries and cursor wrap.
                for index in 0..128 {
                    let (kind, id) = expected[(start + index) % total];
                    if kind == LEASE_BINDING {
                        expected_bindings.push(id);
                    } else {
                        expected_operations.push(id);
                    }
                    probe.values = encode_lease_targets(&expected_bindings, &expected_operations);
                    if probe.encode(1 << 20).unwrap().len() + 35 > frame_bytes {
                        if kind == LEASE_BINDING {
                            expected_bindings.pop();
                        } else {
                            expected_operations.pop();
                        }
                        break;
                    }
                }
                assert_eq!(bindings, expected_bindings);
                assert_eq!(operations, expected_operations);
                assert!(!bindings.is_empty() || !operations.is_empty());
                covered.extend(bindings.into_iter().map(|id| (LEASE_BINDING, id)));
                covered.extend(operations.into_iter().map(|id| (LEASE_OPERATION, id)));
                if covered.len() == total {
                    break;
                }
            }
            assert_eq!(covered.len(), expected.len());
            endpoint.shutdown().await.unwrap();
        }
    }

    #[tokio::test(flavor = "current_thread")]
    async fn renewal_preserves_target_identity_and_cannot_revive_expired_owners() {
        let endpoint = Endpoint::open(
            Handle::current(),
            Arc::new(IdleConnection(Cancellation::default())),
            None,
            EndpointOptions::default(),
        )
        .unwrap();
        let now = Instant::now();
        {
            let mut state = endpoint.state.state.lock().unwrap();
            state
                .outbound_binding_leases
                .insert(1, now + Duration::from_secs(10));
            state
                .outbound_operations
                .insert(1, (now + Duration::from_secs(10), Cancellation::default()));
            state.binding_leases.insert(2, now - Duration::from_secs(1));
        }
        let grant = 60_000_000_000;
        let sent = now - Duration::from_secs(5);
        assert!(
            endpoint
                .state
                .apply_renewal_ack(&[1], &[], sent, grant, &encode_lease_targets(&[], &[1]))
                .is_err()
        );
        endpoint
            .state
            .apply_renewal_ack(&[1], &[], sent, grant, &encode_lease_targets(&[1], &[]))
            .unwrap();
        endpoint
            .state
            .apply_renewal_ack(
                &[1],
                &[],
                sent - Duration::from_secs(1),
                grant,
                &encode_lease_targets(&[1], &[]),
            )
            .unwrap();
        assert_eq!(
            endpoint.state.state.lock().unwrap().outbound_binding_leases[&1],
            sent + Duration::from_secs(60)
        );
        let expired = now - Duration::from_secs(1);
        endpoint
            .state
            .state
            .lock()
            .unwrap()
            .outbound_binding_leases
            .insert(1, expired);
        endpoint
            .state
            .apply_renewal_ack(&[1], &[], now, grant, &encode_lease_targets(&[1], &[]))
            .unwrap();
        assert_eq!(
            endpoint.state.state.lock().unwrap().outbound_binding_leases[&1],
            expired
        );
        let acknowledgement = endpoint
            .state
            .renew_inbound_targets(&encode_lease_targets(&[2], &[]))
            .unwrap();
        assert_eq!(
            decode_lease_targets(&acknowledgement, &Limits::default()).unwrap(),
            (vec![], vec![])
        );
        assert!(
            endpoint
                .state
                .apply_renewal_ack(&[], &[], now, 0, &[0])
                .is_err()
        );
        endpoint.shutdown().await.unwrap();
    }
}
