use serde::*;


#[derive(Serialize, Deserialize, Debug, PartialEq, Clone)]
pub struct SPNStatus {
    #[serde(rename = "Status")]
    pub status: String,
}