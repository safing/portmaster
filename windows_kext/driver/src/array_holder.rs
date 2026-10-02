use alloc::vec::Vec;

pub struct ArrayHolder(Option<Vec<u8>>);

impl ArrayHolder {
    pub const fn default() -> Self {
        Self(None)
    }

    pub fn save(&mut self, data: &[u8]) {
        self.0 = Some(data.to_vec());
    }

    pub fn load(&mut self) -> Option<Vec<u8>> {
        self.0.take()
    }

    pub fn clear(&mut self) {
        self.0 = None;
    }
}

#[cfg(test)]
mod tests {
    use super::ArrayHolder;

    #[test]
    fn clear_discards_saved_fragment() {
        let mut holder = ArrayHolder::default();
        holder.save(&[1, 2, 3]);

        holder.clear();

        assert!(holder.load().is_none());
    }
}
