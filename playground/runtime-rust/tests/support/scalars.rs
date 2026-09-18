use crate::rpctypes::Scalars;
use mini_go::rpc::{Complex32, Complex64};

pub fn scalar_boundaries() -> Scalars {
    Scalars {
        i8: i8::MIN,
        i16: i16::MIN,
        i32: i32::MIN,
        i64: i64::MIN,
        u8: u8::MAX,
        u16: u16::MAX,
        u32: u32::MAX,
        u64: u64::MAX,
        f32: f32::from_bits(0x80000000),
        f64: f64::from_bits(1),
        c64: Complex32 {
            re: f32::INFINITY,
            im: f32::NEG_INFINITY,
        },
        c128: Complex64 {
            re: f64::from_bits(1),
            im: -0.0,
        },
        flags: Some(std::collections::BTreeMap::from([
            (false, "no".into()),
            (true, "yes".into()),
        ])),
        numbers: Some(std::collections::BTreeMap::from([
            (i64::MIN, "low".into()),
            (i64::MAX, "high".into()),
        ])),
        r#type: "keyword".into(),
        next: None,
    }
}
