use super::{Method, ResourceRef, Result, Status};
use std::collections::BTreeMap;

#[derive(Default)]
pub(crate) struct Encoder(pub Vec<u8>);
impl Encoder {
    pub fn uint(&mut self, mut value: u64) {
        while value >= 128 {
            self.0.push(value as u8 | 128);
            value >>= 7;
        }
        self.0.push(value as u8);
    }
    pub fn int(&mut self, value: i64) {
        self.uint(((value << 1) ^ (value >> 63)) as u64);
    }
    pub fn bool(&mut self, value: bool) {
        self.0.push(u8::from(value));
    }
    pub fn raw(&mut self, value: &[u8]) {
        self.uint(value.len() as u64);
        self.0.extend_from_slice(value);
    }
    pub fn string(&mut self, value: &str) {
        self.raw(value.as_bytes());
    }
    pub fn float(&mut self, value: f64) {
        self.0.extend_from_slice(&value.to_bits().to_le_bytes());
    }
    pub fn method(&mut self, method: &Method) {
        for value in [
            &method.id,
            &method.service,
            &method.name,
            &method.contract_hash,
            &method.resource_type_hash,
        ] {
            self.string(value);
        }
    }
    pub fn resource(&mut self, value: Option<&ResourceRef>) {
        self.bool(value.is_some());
        if let Some(value) = value {
            self.uint(value.epoch);
            self.uint(value.object_id);
            self.string(&value.type_hash);
        }
    }
    pub fn labels(&mut self, values: &BTreeMap<String, String>) {
        self.uint(values.len() as u64);
        for (key, value) in values {
            self.string(key);
            self.string(value);
        }
    }
}

pub(crate) struct Decoder<'a> {
    data: &'a [u8],
    offset: usize,
    limit: usize,
}
impl<'a> Decoder<'a> {
    pub fn new(data: &'a [u8], limit: usize) -> Result<Self> {
        if data.len() > limit {
            return Err(Status::protocol("RPC payload byte limit exceeded"));
        }
        Ok(Self {
            data,
            offset: 0,
            limit,
        })
    }
    pub fn remaining(&self) -> usize {
        self.data.len() - self.offset
    }
    pub fn done(&self) -> Result<()> {
        if self.remaining() != 0 {
            return Err(Status::protocol("trailing RPC bytes"));
        }
        Ok(())
    }
    pub fn fixed(&mut self, count: usize) -> Result<&'a [u8]> {
        if count > self.remaining() {
            return Err(Status::protocol("truncated RPC payload"));
        }
        let start = self.offset;
        self.offset += count;
        Ok(&self.data[start..self.offset])
    }
    pub fn uint(&mut self) -> Result<u64> {
        let start = self.offset;
        let mut value = 0;
        for index in 0..10 {
            let byte = *self
                .data
                .get(start + index)
                .ok_or_else(|| Status::protocol("truncated RPC varint"))?;
            if index == 9 && byte > 1 {
                return Err(Status::protocol("overflowing RPC varint"));
            }
            value |= ((byte & 127) as u64) << (index * 7);
            if byte < 128 {
                if index != 0 && byte == 0 {
                    return Err(Status::protocol("non-canonical RPC varint"));
                }
                self.offset += index + 1;
                return Ok(value);
            }
        }
        Err(Status::protocol("overflowing RPC varint"))
    }
    pub fn int(&mut self) -> Result<i64> {
        let value = self.uint()?;
        Ok((value >> 1) as i64 ^ -((value & 1) as i64))
    }
    pub fn bool(&mut self) -> Result<bool> {
        match self.fixed(1)?[0] {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(Status::protocol("invalid RPC bool")),
        }
    }
    pub fn raw(&mut self) -> Result<&'a [u8]> {
        let size = self.uint()?;
        if size > self.limit as u64 || size > self.remaining() as u64 {
            return Err(Status::protocol("invalid RPC byte length"));
        }
        self.fixed(size as usize)
    }
    pub fn string(&mut self) -> Result<String> {
        std::str::from_utf8(self.raw()?)
            .map(str::to_owned)
            .map_err(|_| Status::protocol("invalid RPC UTF-8"))
    }
    pub fn float(&mut self) -> Result<f64> {
        Ok(f64::from_bits(u64::from_le_bytes(
            self.fixed(8)?.try_into().unwrap(),
        )))
    }
    pub fn count(&mut self, limit: usize) -> Result<usize> {
        let count = self.uint()?;
        if count > limit as u64 || count > self.remaining() as u64 {
            return Err(Status::protocol("RPC element limit exceeded"));
        }
        Ok(count as usize)
    }
    pub fn method(&mut self) -> Result<Method> {
        Ok(Method {
            id: self.string()?,
            service: self.string()?,
            name: self.string()?,
            contract_hash: self.string()?,
            resource_type_hash: self.string()?,
        })
    }
    pub fn resource(&mut self) -> Result<Option<ResourceRef>> {
        if !self.bool()? {
            return Ok(None);
        }
        Ok(Some(ResourceRef {
            epoch: self.uint()?,
            object_id: self.uint()?,
            type_hash: self.string()?,
        }))
    }
    pub fn labels(&mut self, limit: usize) -> Result<BTreeMap<String, String>> {
        let count = self.count(limit)?;
        let mut values = BTreeMap::new();
        for _ in 0..count {
            let key = self.string()?;
            let value = self.string()?;
            if key.is_empty() || values.insert(key, value).is_some() {
                return Err(Status::protocol("invalid RPC labels"));
            }
        }
        Ok(values)
    }
}
