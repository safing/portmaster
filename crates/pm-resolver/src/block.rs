use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};

use hickory_proto::op::{Message, ResponseCode};
use hickory_proto::rr::RData;

use crate::model::{
    BLOCK_DETECTION_DISABLED, BLOCK_DETECTION_EMPTY_ANSWER, BLOCK_DETECTION_REFUSED,
    BLOCK_DETECTION_ZERO_IP,
};

/// Decides if the DNS response indicates an upstream block according to the detection rule.
pub fn is_blocked_upstream(detection: &str, answer: &Message) -> bool {
    if detection == BLOCK_DETECTION_DISABLED {
        return false;
    }

    match detection {
        BLOCK_DETECTION_REFUSED => answer.response_code == ResponseCode::Refused,
        BLOCK_DETECTION_ZERO_IP => {
            if answer.response_code != ResponseCode::NoError {
                return false;
            }

            let mut ips: Vec<IpAddr> = Vec::new();
            for record in &answer.answers {
                match &record.data {
                    RData::A(a) => ips.push(IpAddr::V4(a.0)),
                    RData::AAAA(aaaa) => ips.push(IpAddr::V6(aaaa.0)),
                    _ => {}
                }
            }

            if ips.is_empty() {
                return false;
            }

            for ip in ips {
                if !is_zero_ip(ip) {
                    return false;
                }
            }

            true
        }
        BLOCK_DETECTION_EMPTY_ANSWER => {
            answer.response_code == ResponseCode::NXDomain
                && answer.authorities.is_empty()
                && answer.answers.is_empty()
                && answer.additionals.is_empty()
        }
        _ => false,
    }
}

/// Go's `ip.IsUnspecified()` treats an IPv4-mapped `::ffff:0.0.0.0` as the
/// IPv4 zero address too, so canonicalize before comparing.
fn is_zero_ip(ip: IpAddr) -> bool {
    let canonical = ip.to_canonical();
    canonical == IpAddr::V4(Ipv4Addr::UNSPECIFIED)
        || canonical == IpAddr::V6(Ipv6Addr::UNSPECIFIED)
}

#[cfg(test)]
mod tests {
    use super::*;
    use hickory_proto::op::{MessageType, OpCode};
    use hickory_proto::rr::rdata::{A, AAAA};
    use hickory_proto::rr::{Name, Record};
    use std::str::FromStr;

    fn make_test_message(rcode: ResponseCode) -> Message {
        let mut msg = Message::new(0, MessageType::Response, OpCode::Query);
        msg.metadata.response_code = rcode;
        msg
    }

    #[test]
    fn test_refused() {
        let refused_msg = make_test_message(ResponseCode::Refused);
        assert!(is_blocked_upstream(BLOCK_DETECTION_REFUSED, &refused_msg));

        let ok_msg = make_test_message(ResponseCode::NoError);
        assert!(!is_blocked_upstream(BLOCK_DETECTION_REFUSED, &ok_msg));
    }

    #[test]
    fn test_zeroip() {
        let name = Name::from_str("example.com.").unwrap();

        // No answers -> false
        let ok_msg = make_test_message(ResponseCode::NoError);
        assert!(!is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &ok_msg));

        // 0.0.0.0 -> true
        let mut zero_v4_msg = make_test_message(ResponseCode::NoError);
        zero_v4_msg.add_answer(Record::from_rdata(name.clone(), 60, RData::A(A(Ipv4Addr::new(0, 0, 0, 0)))));
        assert!(is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &zero_v4_msg));

        // :: -> true
        let mut zero_v6_msg = make_test_message(ResponseCode::NoError);
        zero_v6_msg.add_answer(Record::from_rdata(name.clone(), 60, RData::AAAA(AAAA(Ipv6Addr::UNSPECIFIED))));
        assert!(is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &zero_v6_msg));

        // ::ffff:0.0.0.0 (IPv4-mapped zero) -> true, like Go's IsUnspecified
        let mut mapped_zero_msg = make_test_message(ResponseCode::NoError);
        mapped_zero_msg.add_answer(Record::from_rdata(name.clone(), 60, RData::AAAA(AAAA("::ffff:0.0.0.0".parse().unwrap()))));
        assert!(is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &mapped_zero_msg));

        // Non-zero IP -> false
        let mut real_ip_msg = make_test_message(ResponseCode::NoError);
        real_ip_msg.add_answer(Record::from_rdata(name.clone(), 60, RData::A(A(Ipv4Addr::new(1, 2, 3, 4)))));
        assert!(!is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &real_ip_msg));

        // Mixed 0.0.0.0 and real IP -> false
        let mut mixed_msg = make_test_message(ResponseCode::NoError);
        mixed_msg.add_answer(Record::from_rdata(name.clone(), 60, RData::A(A(Ipv4Addr::new(0, 0, 0, 0)))));
        mixed_msg.add_answer(Record::from_rdata(name, 60, RData::A(A(Ipv4Addr::new(1, 2, 3, 4)))));
        assert!(!is_blocked_upstream(BLOCK_DETECTION_ZERO_IP, &mixed_msg));
    }

    #[test]
    fn test_empty() {
        // NXDomain and empty -> true
        let empty_nx = make_test_message(ResponseCode::NXDomain);
        assert!(is_blocked_upstream(BLOCK_DETECTION_EMPTY_ANSWER, &empty_nx));

        // NXDomain with answer -> false
        let name = Name::from_str("example.com.").unwrap();
        let mut non_empty_nx = make_test_message(ResponseCode::NXDomain);
        non_empty_nx.add_answer(Record::from_rdata(name, 60, RData::A(A(Ipv4Addr::new(1, 1, 1, 1)))));
        assert!(!is_blocked_upstream(BLOCK_DETECTION_EMPTY_ANSWER, &non_empty_nx));

        // NoError and empty -> false
        let ok_empty = make_test_message(ResponseCode::NoError);
        assert!(!is_blocked_upstream(BLOCK_DETECTION_EMPTY_ANSWER, &ok_empty));
    }

    #[test]
    fn test_disabled() {
        let refused_msg = make_test_message(ResponseCode::Refused);
        assert!(!is_blocked_upstream(BLOCK_DETECTION_DISABLED, &refused_msg));
    }
}
