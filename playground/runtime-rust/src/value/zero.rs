//! Bounded materialization of structured zero values.

use super::{Data, Value};
use crate::{
    contract_generated as wire,
    error::RuntimeError,
    types::{TypeIdentity, TypeRegistry},
};
use std::collections::BTreeMap;

impl Value {
    pub(crate) fn zero_with_budget(
        types: &TypeRegistry,
        typ: &TypeIdentity,
        depth: usize,
        max_depth: usize,
        max_elements: usize,
        remaining: &mut u64,
    ) -> Result<Value, RuntimeError> {
        if depth >= max_depth {
            return Err(RuntimeError::new(
                "value_limit",
                "type",
                "recursive value layout",
            ));
        }
        let underlying = types.underlying(typ)?;
        let data = match underlying {
            TypeIdentity::Primitive(wire::PrimitiveBool) => Data::Bool(false),
            TypeIdentity::Primitive(wire::PrimitiveString) => Data::String((&[][..]).into()),
            TypeIdentity::Primitive(primitive)
                if (wire::PrimitiveInt..=wire::PrimitiveInt64).contains(&primitive) =>
            {
                Data::Integer(0)
            }
            TypeIdentity::Primitive(primitive)
                if (wire::PrimitiveUint..=wire::PrimitiveUintptr).contains(&primitive) =>
            {
                Data::Unsigned(0)
            }
            TypeIdentity::Primitive(wire::PrimitiveFloat32 | wire::PrimitiveFloat64) => {
                Data::Float(0.0)
            }
            TypeIdentity::Primitive(wire::PrimitiveComplex64 | wire::PrimitiveComplex128) => {
                Data::Complex {
                    real: 0.0,
                    imag: 0.0,
                }
            }
            TypeIdentity::Structural { .. } => {
                let (module, node) = types.node(typ)?.unwrap();
                match node.kind {
                    wire::Struct => {
                        *remaining = (node.fields.len() as u64)
                            .checked_mul(16)
                            .and_then(|bytes| bytes.checked_add(128))
                            .and_then(|bytes| remaining.checked_sub(bytes))
                            .ok_or_else(|| {
                                RuntimeError::new(
                                    "allocation_limit",
                                    "zero",
                                    "struct layout exceeds heap budget",
                                )
                            })?;
                        let mut fields = BTreeMap::new();
                        for field in node.fields.iter() {
                            let typ = types.resolve(module, &field.r#type)?;
                            fields.insert(
                                field.name.clone(),
                                Self::zero_with_budget(
                                    types,
                                    &typ,
                                    depth + 1,
                                    max_depth,
                                    max_elements,
                                    remaining,
                                )?,
                            );
                        }
                        Data::Struct(super::StructStorage::zero(fields))
                    }
                    wire::Array => {
                        let length = usize::try_from(node.length)
                            .ok()
                            .filter(|length| *length <= max_elements)
                            .ok_or_else(|| {
                                RuntimeError::new(
                                    "value_limit",
                                    "array",
                                    "array length exceeds limit",
                                )
                            })?;
                        let element = types.resolve(module, &node.elem)?;
                        *remaining = (length as u64)
                            .checked_mul(16)
                            .and_then(|bytes| bytes.checked_add(128))
                            .and_then(|bytes| remaining.checked_sub(bytes))
                            .ok_or_else(|| {
                                RuntimeError::new(
                                    "allocation_limit",
                                    "zero",
                                    "array layout exceeds heap budget",
                                )
                            })?;
                        if length == 0 {
                            Data::Array(Vec::new())
                        } else {
                            let zero = Self::zero_with_budget(
                                types,
                                &element,
                                depth + 1,
                                max_depth,
                                max_elements,
                                remaining,
                            )?;
                            let copies = zero
                                .logical_bytes()?
                                .saturating_sub(16)
                                .checked_mul(length as u64 - 1)
                                .ok_or_else(|| {
                                    RuntimeError::new(
                                        "allocation_limit",
                                        "zero",
                                        "array layout overflow",
                                    )
                                })?;
                            if copies > *remaining {
                                return Err(RuntimeError::new(
                                    "allocation_limit",
                                    "zero",
                                    "array layout exceeds heap budget",
                                ));
                            }
                            let mut values = Vec::new();
                            values.try_reserve_exact(length).map_err(|_| {
                                RuntimeError::new(
                                    "allocation_limit",
                                    "zero",
                                    "array layout allocation failed",
                                )
                            })?;
                            if matches!(zero.data, Data::Struct(_) | Data::Array(_)) {
                                values.push(zero);
                                for _ in 1..length {
                                    values.push(Self::zero_with_budget(
                                        types,
                                        &element,
                                        depth + 1,
                                        max_depth,
                                        max_elements,
                                        remaining,
                                    )?);
                                }
                            } else {
                                *remaining -= copies;
                                values.resize(length, zero);
                            }
                            Data::Array(values)
                        }
                    }
                    _ => Data::Nil,
                }
            }
            _ => Data::Nil,
        };
        Ok(Value {
            typ: typ.clone(),
            data,
        })
    }
}
