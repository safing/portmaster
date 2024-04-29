use core::cell::RefCell;

use alloc::vec::Vec;

pub struct ArrayHolder(RefCell<Option<Vec<u8>>>);
unsafe impl Sync for ArrayHolder {}

impl ArrayHolder {
    pub const fn default() -> Self {
        Self(RefCell::new(None))
    }

    pub fn save(&self, data: &[u8]) {
        if let Ok(mut opt) = self.0.try_borrow_mut() {
            opt.replace(data.to_vec());
        }
    }

    pub fn load(&self) -> Option<Vec<u8>> {
        if let Ok(mut opt) = self.0.try_borrow_mut() {
            return opt.take();
        }
        None
    }
}
