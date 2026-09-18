// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in LICENSE-Go. Adapted from Go's runtime/complex.go.

pub(super) fn complex_divide(a: f64, b: f64, c: f64, d: f64) -> (f64, f64) {
    // Smith's ratio algorithm avoids squaring the denominator components.
    let (mut real, mut imag) = if c.abs() >= d.abs() {
        let ratio = d / c;
        let denominator = c + ratio * d;
        ((a + b * ratio) / denominator, (b - a * ratio) / denominator)
    } else {
        let ratio = c / d;
        let denominator = d + ratio * c;
        ((a * ratio + b) / denominator, (b * ratio - a) / denominator)
    };
    if real.is_nan() && imag.is_nan() {
        if c == 0.0 && d == 0.0 && (!a.is_nan() || !b.is_nan()) {
            real = f64::INFINITY.copysign(c) * a;
            imag = f64::INFINITY.copysign(c) * b;
        } else if (a.is_infinite() || b.is_infinite()) && c.is_finite() && d.is_finite() {
            let a = f64::from(u8::from(a.is_infinite())).copysign(a);
            let b = f64::from(u8::from(b.is_infinite())).copysign(b);
            real = f64::INFINITY * (a * c + b * d);
            imag = f64::INFINITY * (b * c - a * d);
        } else if (c.is_infinite() || d.is_infinite()) && a.is_finite() && b.is_finite() {
            let c = f64::from(u8::from(c.is_infinite())).copysign(c);
            let d = f64::from(u8::from(d.is_infinite())).copysign(d);
            real = 0.0 * (a * c + b * d);
            imag = 0.0 * (b * c - a * d);
        }
    }
    (real, imag)
}
