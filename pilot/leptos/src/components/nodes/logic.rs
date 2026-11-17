//! Pure logic for the nodes component.
//!
//! No Leptos macros live here on purpose: these helpers only deal with node
//! data and strings, which keeps them unit-testable on a native target.

/// Render a node's country code as a compact badge, e.g. `[DE]`.
pub fn country_badge(node: &crate::model::Node) -> String {
    format!("[{}]", node.country)
}

/// Alternating CSS class for a table/list row.
///
/// Zero-based: index `0` is even, index `1` is odd, and so on.
pub fn row_class(index: usize) -> &'static str {
    if index % 2 == 0 {
        "node-row even"
    } else {
        "node-row odd"
    }
}

/// Sort nodes alphabetically by name, using the country code as a tie-breaker.
pub fn sort_nodes(nodes: &mut [crate::model::Node]) {
    nodes.sort_by(|a, b| a.name.cmp(b.name).then_with(|| a.country.cmp(b.country)));
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::model::Node;

    #[test]
    fn country_badge_wraps_code_in_brackets() {
        let de = Node {
            name: "Frankfurt-01",
            country: "DE",
        };
        assert_eq!(country_badge(&de), "[DE]");

        let sg = Node {
            name: "Singapore-02",
            country: "SG",
        };
        assert_eq!(country_badge(&sg), "[SG]");
    }

    #[test]
    fn row_class_alternates_from_even() {
        assert_eq!(row_class(0), "node-row even");
        assert_eq!(row_class(1), "node-row odd");
        assert_eq!(row_class(2), "node-row even");
        assert_eq!(row_class(3), "node-row odd");
        assert_eq!(row_class(42), "node-row even");
        assert_eq!(row_class(43), "node-row odd");
    }

    #[test]
    fn sort_nodes_orders_by_name() {
        let mut nodes = vec![
            Node {
                name: "Singapore-02",
                country: "SG",
            },
            Node {
                name: "Frankfurt-01",
                country: "DE",
            },
            Node {
                name: "Amsterdam-03",
                country: "NL",
            },
        ];

        sort_nodes(&mut nodes);

        let names: Vec<_> = nodes.iter().map(|n| n.name).collect();
        assert_eq!(names, ["Amsterdam-03", "Frankfurt-01", "Singapore-02"]);
    }

    #[test]
    fn sort_nodes_uses_country_as_tie_breaker() {
        let mut nodes = vec![
            Node {
                name: "Alpha",
                country: "US",
            },
            Node {
                name: "Beta",
                country: "DE",
            },
            Node {
                name: "Alpha",
                country: "DE",
            },
        ];

        sort_nodes(&mut nodes);

        let pairs: Vec<_> = nodes.iter().map(|n| (n.name, n.country)).collect();
        assert_eq!(
            pairs,
            [("Alpha", "DE"), ("Alpha", "US"), ("Beta", "DE")]
        );
    }

    #[test]
    fn sort_nodes_handles_empty_and_single_element() {
        let mut empty: Vec<Node> = Vec::new();
        sort_nodes(&mut empty);
        assert!(empty.is_empty());

        let mut one = vec![Node {
            name: "Only",
            country: "FR",
        }];
        sort_nodes(&mut one);
        assert_eq!(one[0].name, "Only");
    }
}
