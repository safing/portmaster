use serde::*;
use super::super::message::Payload;

#[derive(Serialize, Deserialize, Debug, PartialEq, Clone)]
pub struct BooleanValue {
    #[serde(rename = "Value")]
    pub value: Option<bool>,
}

impl TryInto<Payload> for BooleanValue {
    type Error = serde_json::Error;

    fn try_into(self) -> Result<Payload, Self::Error> {
        let str = serde_json::to_string(&self)?;

        Ok(Payload::JSON(str))
    }
}