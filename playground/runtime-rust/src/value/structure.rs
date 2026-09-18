//! Struct value copies share immutable fields until a destination is written.

use super::Value;
use std::{
    collections::{BTreeMap, BTreeSet},
    ops::{Deref, DerefMut},
    sync::Arc,
};

#[derive(Clone, Debug)]
pub struct StructStorage(Arc<StructFields>);

#[derive(Clone, Debug)]
struct StructFields {
    values: BTreeMap<String, Value>,
    initialized: BTreeSet<String>,
}

impl StructStorage {
    pub(crate) fn identity(&self) -> usize {
        Arc::as_ptr(&self.0) as usize
    }
    pub(crate) fn zero(values: BTreeMap<String, Value>) -> Self {
        Self(Arc::new(StructFields {
            values,
            initialized: BTreeSet::new(),
        }))
    }
    pub(crate) fn initialized_values(&self) -> impl Iterator<Item = &Value> {
        self.0
            .initialized
            .iter()
            .filter_map(|name| self.0.values.get(name))
    }
    pub(crate) fn guest_slots(&self) -> usize {
        if self.0.initialized.is_empty() {
            0
        } else {
            self.0.values.len()
        }
    }
    pub fn get_mut(&mut self, name: &str) -> Option<&mut Value> {
        let fields = Arc::make_mut(&mut self.0);
        if fields.values.contains_key(name) {
            fields.initialized.insert(name.to_owned());
        }
        fields.values.get_mut(name)
    }
}

impl From<BTreeMap<String, Value>> for StructStorage {
    fn from(fields: BTreeMap<String, Value>) -> Self {
        let initialized = fields.keys().cloned().collect();
        Self(Arc::new(StructFields {
            values: fields,
            initialized,
        }))
    }
}

impl Deref for StructStorage {
    type Target = BTreeMap<String, Value>;

    fn deref(&self) -> &Self::Target {
        &self.0.values
    }
}

impl DerefMut for StructStorage {
    fn deref_mut(&mut self) -> &mut Self::Target {
        let fields = Arc::make_mut(&mut self.0);
        fields.initialized.extend(fields.values.keys().cloned());
        &mut fields.values
    }
}

impl IntoIterator for StructStorage {
    type Item = (String, Value);
    type IntoIter = std::collections::btree_map::IntoIter<String, Value>;

    fn into_iter(self) -> Self::IntoIter {
        Arc::unwrap_or_clone(self.0).values.into_iter()
    }
}

impl<'a> IntoIterator for &'a StructStorage {
    type Item = (&'a String, &'a Value);
    type IntoIter = std::collections::btree_map::Iter<'a, String, Value>;

    fn into_iter(self) -> Self::IntoIter {
        self.0.values.iter()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{types::TypeIdentity, value::Data};

    #[test]
    fn nested_arrays_and_fields_are_independent_after_value_copy() {
        let original = StructStorage::from(BTreeMap::from([
            ("Name".into(), Value::string(b"original".to_vec())),
            (
                "Items".into(),
                Value {
                    typ: TypeIdentity::Any,
                    data: Data::Array(vec![Value::int(42)]),
                },
            ),
        ]));
        let mut copy = original.clone();
        let Data::Array(items) = &mut copy.get_mut("Items").unwrap().data else {
            panic!("expected array")
        };
        items[0] = Value::int(99);
        copy.insert("Name".into(), Value::string(b"copy".to_vec()));
        let Data::Array(items) = &original["Items"].data else {
            panic!("expected array")
        };
        assert_eq!(items[0].integer().unwrap(), 42);
        assert_eq!(original["Name"].to_string(), "original");
        assert_eq!(copy["Name"].to_string(), "copy");
    }
}
