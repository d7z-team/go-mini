use super::wire::{Decoder, Encoder};
use super::{Limits, Result, Status};
use std::collections::BTreeSet;

#[derive(Clone, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub struct ResourceRef {
    pub epoch: u64,
    pub object_id: u64,
    pub type_hash: String,
}
impl ResourceRef {
    pub fn validate(&self) -> Result<()> {
        if self.epoch == 0 || self.object_id == 0 || self.type_hash.len() != 64 {
            return Err(Status::protocol("invalid RPC resource reference"));
        }
        Ok(())
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct Value {
    pub typ: String,
    pub data: Data,
}
#[derive(Clone, Debug, PartialEq)]
pub enum Data {
    Nil,
    Bool(bool),
    Int(i64),
    Uint(u64),
    Float(f64),
    Complex(f64, f64),
    String(String),
    Bytes(Vec<u8>),
    Slice(Vec<Value>),
    Map(Vec<MapEntry>),
    Struct(Vec<Field>),
    Optional(Box<Value>),
    Resource(ResourceRef),
}
#[derive(Clone, Debug, PartialEq)]
pub struct MapEntry {
    pub key: Value,
    pub value: Value,
}
#[derive(Clone, Debug, PartialEq)]
pub struct Field {
    pub id: u32,
    pub value: Value,
}
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct Complex32 {
    pub re: f32,
    pub im: f32,
}
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct Complex64 {
    pub re: f64,
    pub im: f64,
}
impl Value {
    pub fn new(typ: impl Into<String>, data: Data) -> Self {
        Self {
            typ: typ.into(),
            data,
        }
    }
    pub fn resource(reference: ResourceRef) -> Self {
        Self::new(reference.type_hash.clone(), Data::Resource(reference))
    }
    pub fn expect_type(&self, typ: &str) -> Result<()> {
        if self.typ != typ {
            return Err(Status::protocol(format!(
                "expected {typ}, received {}",
                self.typ
            )));
        }
        Ok(())
    }
}

pub fn validate_values(values: &[Value], limits: &Limits) -> Result<()> {
    let mut work: Vec<_> = values.iter().rev().map(|value| (value, 0)).collect();
    let mut elements = 0usize;
    let mut bytes = 0usize;
    while let Some((value, depth)) = work.pop() {
        elements += 1;
        bytes = bytes.saturating_add(value.typ.len());
        if depth > limits.max_value_depth
            || elements > limits.max_value_elements
            || bytes > limits.max_message_bytes
        {
            return Err(Status::exhausted("RPC value limit exceeded"));
        }
        let typ = value.typ.trim();
        if value.typ.is_empty() {
            return Err(Status::protocol("RPC value has no type"));
        }
        if let Data::Resource(reference) = &value.data {
            reference.validate()?;
            if depth != 0 || typ != reference.type_hash {
                return Err(Status::protocol("invalid RPC resource value"));
            }
            continue;
        }
        let valid = if matches!(value.data, Data::Nil) {
            true
        } else if let Some(elem) = typ
            .strip_prefix("optional[")
            .and_then(|t| t.strip_suffix(']'))
        {
            matches!(&value.data, Data::Optional(value) if value.typ.trim() == elem.trim())
        } else if let Some(elem) = typ.strip_prefix("[]") {
            if elem == "uint8" {
                matches!(value.data, Data::Bytes(_))
            } else {
                matches!(&value.data, Data::Slice(values) if values.iter().all(|v| v.typ.trim() == elem.trim()))
            }
        } else if let Some(rest) = typ.strip_prefix("map[") {
            let mut depth = 1;
            let split = rest.char_indices().find_map(|(index, ch)| {
                if ch == '[' {
                    depth += 1;
                } else if ch == ']' {
                    depth -= 1;
                }
                (depth == 0).then_some(index)
            });
            split.is_some_and(|index| {
                let (key, elem) = (&rest[..index], &rest[index+1..]);
                !key.trim().is_empty() && !elem.trim().is_empty() && matches!(&value.data, Data::Map(entries) if entries.iter().all(|entry| entry.key.typ.trim() == key.trim() && entry.value.typ.trim() == elem.trim()))
            })
        } else {
            match typ {
                "bool" => matches!(value.data, Data::Bool(_)),
                "string" => matches!(value.data, Data::String(_)),
                "int8" | "int16" | "int32" | "int64" => matches!(value.data, Data::Int(_)),
                "uint8" | "uint16" | "uint32" | "uint64" => matches!(value.data, Data::Uint(_)),
                "float32" | "float64" => matches!(value.data, Data::Float(_)),
                "complex64" | "complex128" => matches!(value.data, Data::Complex(_, _)),
                _ => {
                    matches!(value.data, Data::Struct(_))
                        || matches!(value.data, Data::Int(v) if typ.contains('.') && i32::try_from(v).is_ok())
                }
            }
        };
        if !valid {
            return Err(Status::protocol(format!("invalid RPC value shape: {typ}")));
        }
        match &value.data {
            Data::String(value) => bytes = bytes.saturating_add(value.len()),
            Data::Bytes(value) => bytes = bytes.saturating_add(value.len()),
            Data::Slice(values) => work.extend(values.iter().rev().map(|v| (v, depth + 1))),
            Data::Map(entries) => {
                for entry in entries.iter().rev() {
                    work.push((&entry.value, depth + 1));
                    work.push((&entry.key, depth + 1));
                }
            }
            Data::Struct(fields) => {
                let mut seen = BTreeSet::new();
                for field in fields.iter().rev() {
                    if field.id == 0 || !seen.insert(field.id) {
                        return Err(Status::protocol("invalid or duplicate RPC field ID"));
                    }
                    work.push((&field.value, depth + 1));
                }
            }
            Data::Optional(value) => work.push((value, depth + 1)),
            _ => {}
        }
        if bytes > limits.max_message_bytes
            || work.len() > limits.max_value_elements.saturating_sub(elements)
        {
            return Err(Status::exhausted("RPC value size limit exceeded"));
        }
    }
    Ok(())
}

pub fn encode_values(values: &[Value], limits: &Limits) -> Result<Vec<u8>> {
    validate_values(values, limits)?;
    let mut encoder = Encoder::default();
    encoder.uint(values.len() as u64);
    for value in values {
        encode_value(&mut encoder, value);
    }
    if encoder.0.len() > limits.max_message_bytes {
        return Err(Status::exhausted("RPC encoded bytes limit exceeded"));
    }
    Ok(encoder.0)
}

fn encode_value(encoder: &mut Encoder, value: &Value) {
    encoder.string(&value.typ);
    match &value.data {
        Data::Nil => encoder.uint(0),
        Data::Bool(v) => {
            encoder.uint(1);
            encoder.bool(*v);
        }
        Data::Int(v) => {
            encoder.uint(2);
            encoder.int(*v);
        }
        Data::Uint(v) => {
            encoder.uint(3);
            encoder.uint(*v);
        }
        Data::Float(v) => {
            encoder.uint(4);
            encoder.float(*v);
        }
        Data::Complex(r, i) => {
            encoder.uint(5);
            encoder.float(*r);
            encoder.float(*i);
        }
        Data::String(v) => {
            encoder.uint(6);
            encoder.string(v);
        }
        Data::Bytes(v) => {
            encoder.uint(7);
            encoder.raw(v);
        }
        Data::Slice(v) => {
            encoder.uint(8);
            encoder.uint(v.len() as u64);
            for v in v {
                encode_value(encoder, v);
            }
        }
        Data::Map(v) => {
            encoder.uint(9);
            encoder.uint(v.len() as u64);
            let mut entries: Vec<_> = v
                .iter()
                .map(|entry| {
                    let mut key = Encoder::default();
                    encode_value(&mut key, &entry.key);
                    (key.0, &entry.value)
                })
                .collect();
            entries.sort_by(|a, b| a.0.cmp(&b.0));
            for (key, value) in entries {
                encoder.0.extend(key);
                encode_value(encoder, value);
            }
        }
        Data::Struct(v) => {
            encoder.uint(10);
            encoder.uint(v.len() as u64);
            let mut fields: Vec<_> = v.iter().collect();
            fields.sort_by_key(|field| field.id);
            for field in fields {
                encoder.uint(field.id as u64);
                encode_value(encoder, &field.value);
            }
        }
        Data::Optional(v) => {
            encoder.uint(11);
            encode_value(encoder, v);
        }
        Data::Resource(v) => {
            encoder.uint(12);
            encoder.uint(v.epoch);
            encoder.uint(v.object_id);
            encoder.string(&v.type_hash);
        }
    }
}

pub fn decode_values(payload: &[u8], limits: &Limits) -> Result<Vec<Value>> {
    let mut decoder = Decoder::new(payload, limits.max_message_bytes)?;
    let count = decoder.count(limits.max_value_elements)?;
    let mut remaining = limits.max_value_elements;
    let mut values = Vec::new();
    for _ in 0..count {
        values.push(decode_value(&mut decoder, 0, limits, &mut remaining)?);
    }
    decoder.done()?;
    validate_values(&values, limits)?;
    Ok(values)
}

fn decode_value(
    decoder: &mut Decoder<'_>,
    depth: usize,
    limits: &Limits,
    remaining: &mut usize,
) -> Result<Value> {
    if depth > limits.max_value_depth || *remaining == 0 {
        return Err(Status::protocol("RPC value limit exceeded"));
    }
    *remaining -= 1;
    let typ = decoder.string()?;
    let kind = decoder.uint()?;
    let data = match kind {
        0 => Data::Nil,
        1 => Data::Bool(decoder.bool()?),
        2 => Data::Int(decoder.int()?),
        3 => Data::Uint(decoder.uint()?),
        4 => Data::Float(decoder.float()?),
        5 => Data::Complex(decoder.float()?, decoder.float()?),
        6 => Data::String(decoder.string()?),
        7 => Data::Bytes(decoder.raw()?.to_vec()),
        8 => {
            let count = decoder.count(*remaining)?;
            let mut values = Vec::new();
            for _ in 0..count {
                values.push(decode_value(decoder, depth + 1, limits, remaining)?);
            }
            Data::Slice(values)
        }
        9 => {
            let count = decoder.count(*remaining / 2)?;
            let mut entries = Vec::new();
            for _ in 0..count {
                entries.push(MapEntry {
                    key: decode_value(decoder, depth + 1, limits, remaining)?,
                    value: decode_value(decoder, depth + 1, limits, remaining)?,
                });
            }
            Data::Map(entries)
        }
        10 => {
            let count = decoder.count(*remaining)?;
            let mut fields = Vec::new();
            for _ in 0..count {
                let id = u32::try_from(decoder.uint()?)
                    .map_err(|_| Status::protocol("invalid RPC field ID"))?;
                fields.push(Field {
                    id,
                    value: decode_value(decoder, depth + 1, limits, remaining)?,
                });
            }
            Data::Struct(fields)
        }
        11 => Data::Optional(Box::new(decode_value(
            decoder,
            depth + 1,
            limits,
            remaining,
        )?)),
        12 => Data::Resource(ResourceRef {
            epoch: decoder.uint()?,
            object_id: decoder.uint()?,
            type_hash: decoder.string()?,
        }),
        _ => return Err(Status::protocol("unknown RPC value kind")),
    };
    Ok(Value { typ, data })
}
