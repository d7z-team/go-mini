//! Owned FFI and Endpoint envelopes. Decoding consumes one complete bounded message.
use super::wire::{Decoder, Encoder};
use super::*;

#[derive(Clone, Debug, Default)]
pub struct Frame {
    pub kind: u64,
    pub protocol: String,
    pub limits: Option<Limits>,
    pub origin: String,
    pub id: u64,
    pub target_id: u64,
    pub binding: u64,
    pub reply: bool,
    pub epoch: u64,
    pub contract: Option<Contract>,
    pub options: BindOptions,
    pub hops: i64,
    pub call: Option<WireCall>,
    pub reference: Option<ResourceRef>,
    pub values: Vec<u8>,
    pub accept: bool,
    pub code: String,
    pub message: String,
    pub timeout: i64,
    pub lease_ttl: i64,
    pub admission_timeout: i64,
    pub max_call_duration: i64,
}
#[derive(Clone, Debug)]
pub struct WireCall {
    pub method: Method,
    pub receiver: Option<ResourceRef>,
    pub arguments: Vec<u8>,
}

pub const HELLO: u64 = 1;
pub const READY: u64 = 2;
pub const BIND: u64 = 3;
pub const CALL: u64 = 4;
pub const DECISION: u64 = 5;
pub const DROP: u64 = 6;
pub const CLOSE: u64 = 7;
pub const CANCEL: u64 = 8;
pub const RENEW: u64 = 10;
pub const RENEW_ACK: u64 = 11;
pub const ACCEPTED: u64 = 12;
pub const OFFER: u64 = 13;
pub const DONE: u64 = 14;
pub const LEASE_BINDING: u8 = 1;
pub const LEASE_OPERATION: u8 = 2;

impl Limits {
    pub(crate) fn wire_values(&self) -> [usize; 11] {
        [
            self.max_frame_bytes,
            self.max_message_bytes,
            self.max_in_flight_bytes,
            self.max_bindings,
            self.max_pending_calls,
            self.max_pending_results,
            self.max_pending_controls,
            self.max_resources,
            self.max_methods,
            self.max_value_depth,
            self.max_value_elements,
        ]
    }
    fn from_wire(v: [usize; 11]) -> Self {
        Self {
            max_frame_bytes: v[0],
            max_message_bytes: v[1],
            max_in_flight_bytes: v[2],
            max_bindings: v[3],
            max_pending_calls: v[4],
            max_pending_results: v[5],
            max_pending_controls: v[6],
            max_resources: v[7],
            max_methods: v[8],
            max_value_depth: v[9],
            max_value_elements: v[10],
        }
    }
    pub fn validate_endpoint(&self) -> Result<()> {
        if self.max_frame_bytes < 256
            || self.max_in_flight_bytes < self.max_message_bytes
            || self.wire_values().contains(&0)
        {
            return Err(Status::new(
                "invalid_argument",
                "invalid RPC endpoint limits",
            ));
        }
        Ok(())
    }
    pub(crate) fn intersect(&self, other: &Self) -> Self {
        let mut values = self.wire_values();
        for (value, peer) in values.iter_mut().zip(other.wire_values()) {
            *value = (*value).min(peer);
        }
        Self::from_wire(values)
    }
}

impl Frame {
    pub fn validate(&self) -> Result<()> {
        let valid = if self.origin.is_empty()
            || self.lease_ttl < 0
            || self.admission_timeout < 0
            || self.max_call_duration < 0
        {
            false
        } else if self.kind == HELLO {
            !self.reply
                && !self.protocol.is_empty()
                && self.id == 0
                && self.target_id == 0
                && self
                    .limits
                    .as_ref()
                    .is_some_and(|l| l.validate_endpoint().is_ok())
                && self.lease_ttl >= 1_000_000
                && self.admission_timeout >= 1_000_000
                && self.max_call_duration >= 0
        } else if self.kind == READY {
            !self.reply && self.id == 0 && self.target_id == 0
        } else if !(BIND..=DONE).contains(&self.kind) {
            false
        } else if self.reply {
            self.target_id != 0
                && self.id == 0
                && matches!(self.kind, RENEW_ACK | ACCEPTED | OFFER | DONE)
                && self.kind != CANCEL
                && (self.kind != ACCEPTED || self.code.is_empty())
                && (self.kind != OFFER || !self.code.is_empty() || self.binding != 0)
        } else if self.timeout < 0 {
            false
        } else if self.kind == CANCEL {
            self.id == 0 && self.target_id != 0
        } else if self.id == 0 || self.target_id != 0 && self.kind != DECISION {
            false
        } else {
            match self.kind {
                BIND => self
                    .contract
                    .as_ref()
                    .is_some_and(|c| c.protocol == CONTRACT_PROTOCOL && !c.methods.is_empty()),
                CALL => self.binding != 0 && self.call.is_some(),
                DECISION => self.binding != 0 && self.target_id != 0,
                DROP => self.binding != 0 && self.reference.is_some(),
                CLOSE => self.binding != 0,
                RENEW => self.binding == 0 && self.call.is_none() && self.contract.is_none(),
                _ => false,
            }
        };
        if !valid {
            return Err(Status::protocol("invalid RPC frame"));
        }
        Ok(())
    }
    pub fn encode(&self, limit: usize) -> Result<Vec<u8>> {
        self.validate()?;
        let mut e = Encoder(b"MGRP\x01".to_vec());
        e.uint(self.kind);
        e.string(&self.protocol);
        e.bool(self.limits.is_some());
        if let Some(limits) = &self.limits {
            for value in limits.wire_values() {
                e.uint(value as u64);
            }
        }
        e.int(self.lease_ttl);
        e.int(self.admission_timeout);
        e.int(self.max_call_duration);
        e.string(&self.origin);
        e.uint(self.id);
        e.uint(self.target_id);
        e.uint(self.binding);
        e.bool(self.reply);
        e.uint(self.epoch);
        e.bool(self.contract.is_some());
        if let Some(contract) = &self.contract {
            e.string(&contract.protocol);
            e.uint(contract.methods.len() as u64);
            for method in &contract.methods {
                e.method(method);
            }
        }
        e.string(&self.options.affinity_key);
        e.labels(&self.options.labels);
        e.int(self.hops);
        e.bool(self.call.is_some());
        if let Some(call) = &self.call {
            e.method(&call.method);
            e.resource(call.receiver.as_ref());
            e.raw(&call.arguments);
        }
        e.resource(self.reference.as_ref());
        e.raw(&self.values);
        e.bool(self.accept);
        e.string(&self.code);
        e.string(&self.message);
        e.int(self.timeout);
        if e.0.len() > limit {
            return Err(Status::exhausted("RPC frame message limit exceeded"));
        }
        Ok(e.0)
    }
    pub fn decode(payload: &[u8], limits: &Limits) -> Result<Self> {
        let mut d = Decoder::new(payload, limits.max_message_bytes)?;
        if d.fixed(5)? != b"MGRP\x01" {
            return Err(Status::protocol("invalid RPC frame header"));
        }
        let kind = d.uint()?;
        let protocol = d.string()?;
        let peer_limits = if d.bool()? {
            let mut values = [0; 11];
            for value in &mut values {
                *value = usize::try_from(d.uint()?)
                    .map_err(|_| Status::protocol("RPC limit overflow"))?;
            }
            Some(Limits::from_wire(values))
        } else {
            None
        };
        let lease_ttl = d.int()?;
        let admission_timeout = d.int()?;
        let max_call_duration = d.int()?;
        let origin = d.string()?;
        let id = d.uint()?;
        let target_id = d.uint()?;
        let binding = d.uint()?;
        let reply = d.bool()?;
        let epoch = d.uint()?;
        let contract = if d.bool()? {
            let protocol = d.string()?;
            let count = d.count(limits.max_methods)?;
            let mut methods = Vec::new();
            for _ in 0..count {
                methods.push(d.method()?);
            }
            Some(Contract { protocol, methods })
        } else {
            None
        };
        let options = BindOptions {
            affinity_key: d.string()?,
            labels: d.labels(limits.max_methods)?,
        };
        let hops = d.int()?;
        let call = if d.bool()? {
            Some(WireCall {
                method: d.method()?,
                receiver: d.resource()?,
                arguments: d.raw()?.to_vec(),
            })
        } else {
            None
        };
        let frame = Self {
            kind,
            protocol,
            limits: peer_limits,
            origin,
            id,
            target_id,
            binding,
            reply,
            epoch,
            contract,
            options,
            hops,
            call,
            reference: d.resource()?,
            values: d.raw()?.to_vec(),
            accept: d.bool()?,
            code: d.string()?,
            message: d.string()?,
            timeout: d.int()?,
            lease_ttl,
            admission_timeout,
            max_call_duration,
        };
        d.done()?;
        frame.validate()?;
        Ok(frame)
    }
}

/// Reassembles one data message at a time. A control message uses ID zero and
/// is always a complete single fragment, so it can pass between data chunks.
#[derive(Default)]
pub struct Assembly {
    previous: u64,
    current: u64,
    total: usize,
    data: Vec<u8>,
    control: bool,
}
impl Assembly {
    pub fn has_pending_data(&self) -> bool {
        self.current != 0
    }
    pub fn last_was_control(&self) -> bool {
        self.control
    }
    pub fn push(&mut self, payload: &[u8], limits: &Limits) -> Result<Option<Vec<u8>>> {
        let mut d = Decoder::new(payload, limits.max_frame_bytes)?;
        if d.fixed(5)? != b"MGRF\x01" {
            return Err(Status::protocol("invalid RPC fragment header"));
        }
        let id = d.uint()?;
        let total = d.uint()?;
        let offset = d.uint()?;
        if total == 0
            || total > limits.max_message_bytes as u64
            || total > limits.max_in_flight_bytes as u64
            || offset > total
            || d.remaining() == 0
            || d.remaining() as u64 > total - offset
        {
            return Err(Status::protocol("invalid RPC fragment bounds"));
        }
        let bytes = d.fixed(d.remaining())?;
        let total = total as usize;
        if id == 0 {
            if offset != 0 || bytes.len() != total {
                return Err(Status::protocol("RPC control fragment must be complete"));
            }
            self.control = true;
            return Ok(Some(bytes.to_vec()));
        }
        if self.current == 0 {
            if offset != 0 || self.previous.checked_add(1) != Some(id) {
                return Err(Status::protocol(
                    "RPC fragment sequence is not strictly increasing",
                ));
            }
            self.current = id;
            self.total = total;
        }
        if self.current != id || self.total != total || self.data.len() != offset as usize {
            return Err(Status::protocol("RPC fragment is not contiguous"));
        }
        self.control = false;
        self.data.extend_from_slice(bytes);
        if self.data.len() != self.total {
            return Ok(None);
        }
        self.previous = id;
        self.current = 0;
        self.total = 0;
        self.control = false;
        Ok(Some(std::mem::take(&mut self.data)))
    }
}

pub fn encode_lease_targets(bindings: &[u64], operations: &[u64]) -> Vec<u8> {
    let mut e = Encoder::default();
    e.uint((bindings.len() + operations.len()) as u64);
    for id in bindings {
        e.0.push(LEASE_BINDING);
        e.uint(*id);
    }
    for id in operations {
        e.0.push(LEASE_OPERATION);
        e.uint(*id);
    }
    e.0
}

pub fn decode_lease_targets(payload: &[u8], limits: &Limits) -> Result<(Vec<u64>, Vec<u64>)> {
    let mut d = Decoder::new(payload, limits.max_message_bytes)?;
    let count = d.count(limits.max_pending_controls)?;
    let mut bindings = Vec::new();
    let mut operations = Vec::new();
    let mut seen_bindings = std::collections::BTreeSet::new();
    let mut seen_operations = std::collections::BTreeSet::new();
    for _ in 0..count {
        let kind = d.fixed(1)?[0];
        let id = d.uint()?;
        if id == 0 {
            return Err(Status::protocol("invalid RPC lease target id"));
        }
        match kind {
            LEASE_BINDING if seen_bindings.insert(id) => bindings.push(id),
            LEASE_OPERATION if seen_operations.insert(id) => operations.push(id),
            LEASE_BINDING | LEASE_OPERATION => {
                return Err(Status::protocol("duplicate RPC lease target"));
            }
            _ => return Err(Status::protocol("invalid RPC lease target kind")),
        }
    }
    d.done()?;
    Ok((bindings, operations))
}

pub fn encode_fragment(
    id: u64,
    total: usize,
    offset: usize,
    data: &[u8],
    limit: usize,
) -> Result<Vec<u8>> {
    if total == 0 || data.is_empty() || offset > total || data.len() > total - offset {
        return Err(Status::protocol("invalid RPC fragment"));
    }
    if id == 0 && (offset != 0 || data.len() != total) {
        return Err(Status::protocol("invalid RPC fragment"));
    }
    let mut e = Encoder(b"MGRF\x01".to_vec());
    e.uint(id);
    e.uint(total as u64);
    e.uint(offset as u64);
    e.0.extend_from_slice(data);
    if e.0.len() > limit {
        return Err(Status::exhausted("RPC fragment exceeds frame limit"));
    }
    Ok(e.0)
}

pub fn encode_control_fragment(data: &[u8], limit: usize) -> Result<Vec<u8>> {
    encode_fragment(0, data.len(), 0, data, limit)
}

#[derive(Clone, Debug)]
pub struct FfiRequest {
    pub version: String,
    pub operation: String,
    pub target: String,
    pub lease: u64,
    pub contract: Contract,
    pub options: BindOptions,
    pub method: Method,
    pub receiver: Option<ResourceRef>,
    pub payload: Vec<u8>,
    pub resource: Option<ResourceRef>,
    pub request_id: u64,
    pub code: String,
    pub message: String,
    pub timeout_nanos: i64,
}
impl Default for FfiRequest {
    fn default() -> Self {
        Self {
            version: FFI_PROTOCOL.into(),
            operation: String::new(),
            target: String::new(),
            lease: 0,
            contract: Contract {
                protocol: String::new(),
                methods: Vec::new(),
            },
            options: BindOptions::default(),
            method: Method::default(),
            receiver: None,
            payload: Vec::new(),
            resource: None,
            request_id: 0,
            code: String::new(),
            message: String::new(),
            timeout_nanos: 0,
        }
    }
}
impl FfiRequest {
    pub fn decode(payload: &[u8], limits: &Limits) -> Result<Self> {
        let mut d = Decoder::new(payload, limits.max_message_bytes)?;
        let version = d.string()?;
        let operation = d.string()?;
        let target = d.string()?;
        let lease = d.uint()?;
        let protocol = d.string()?;
        let count = d.count(limits.max_methods)?;
        let mut methods = Vec::new();
        for _ in 0..count {
            methods.push(d.method()?);
        }
        let request = Self {
            version,
            operation,
            target,
            lease,
            contract: Contract { protocol, methods },
            options: BindOptions {
                affinity_key: d.string()?,
                labels: d.labels(limits.max_methods)?,
            },
            method: d.method()?,
            receiver: d.resource()?,
            payload: d.raw()?.to_vec(),
            resource: d.resource()?,
            request_id: d.uint()?,
            code: d.string()?,
            message: d.string()?,
            timeout_nanos: d.int()?,
        };
        d.done()?;
        Ok(request)
    }
    pub fn encode(&self) -> Vec<u8> {
        let mut e = Encoder::default();
        e.string(&self.version);
        e.string(&self.operation);
        e.string(&self.target);
        e.uint(self.lease);
        e.string(&self.contract.protocol);
        e.uint(self.contract.methods.len() as u64);
        for method in &self.contract.methods {
            e.method(method);
        }
        e.string(&self.options.affinity_key);
        e.labels(&self.options.labels);
        e.method(&self.method);
        e.resource(self.receiver.as_ref());
        e.raw(&self.payload);
        e.resource(self.resource.as_ref());
        e.uint(self.request_id);
        e.string(&self.code);
        e.string(&self.message);
        e.int(self.timeout_nanos);
        e.0
    }
}

#[derive(Clone, Debug)]
pub struct FfiResponse {
    pub version: String,
    pub operation: String,
    pub lease: u64,
    pub request_id: u64,
    pub method: Method,
    pub receiver: Option<ResourceRef>,
    pub payload: Vec<u8>,
    pub code: String,
    pub message: String,
}
impl Default for FfiResponse {
    fn default() -> Self {
        Self {
            version: FFI_PROTOCOL.into(),
            operation: String::new(),
            lease: 0,
            request_id: 0,
            method: Method::default(),
            receiver: None,
            payload: Vec::new(),
            code: String::new(),
            message: String::new(),
        }
    }
}
impl FfiResponse {
    pub fn encode(&self) -> Vec<u8> {
        let mut e = Encoder::default();
        e.string(&self.version);
        e.string(&self.operation);
        e.uint(self.lease);
        e.uint(self.request_id);
        e.method(&self.method);
        e.resource(self.receiver.as_ref());
        e.raw(&self.payload);
        e.string(&self.code);
        e.string(&self.message);
        e.0
    }
    pub fn decode(payload: &[u8], limits: &Limits) -> Result<Self> {
        let mut d = Decoder::new(payload, limits.max_message_bytes)?;
        let response = Self {
            version: d.string()?,
            operation: d.string()?,
            lease: d.uint()?,
            request_id: d.uint()?,
            method: d.method()?,
            receiver: d.resource()?,
            payload: d.raw()?.to_vec(),
            code: d.string()?,
            message: d.string()?,
        };
        d.done()?;
        Ok(response)
    }
}
