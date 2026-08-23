pub mod core {
    pub mod v1 {
        include!(concat!(env!("OUT_DIR"), "/laminara.core.v1.rs"));
    }
}

pub mod api {
    pub mod v1 {
        include!(concat!(env!("OUT_DIR"), "/laminara.api.v1.rs"));
    }
}
