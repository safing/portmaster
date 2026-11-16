//! Port of `decideOnConnection` and `runDeciders` of Go `master.go`.

use pm_network::{Connection, ConnectionType, Verdict};
use pm_profile::{
    options::CFG_OPTION_DEFAULT_ACTION_KEY, LayeredProfile, DEFAULT_ACTION_ASK,
    DEFAULT_ACTION_NOT_SET, DEFAULT_ACTION_PERMIT,
};

use crate::deciders::{Decider, DEFAULT_DECIDERS, NO_REASON_OPTION_KEY};
use crate::env::FirewallEnv;

/// Makes a decision about a connection.
///
/// A missing profile (Go: no process or no layered profile) denies the
/// connection. A connection without an entity is denied too; Go would panic.
pub fn decide_on_connection(
    conn: &mut Connection,
    profile: Option<&LayeredProfile>,
    env: &dyn FirewallEnv,
) {
    let Some(profile) = profile else {
        conn.deny("unknown process or profile", NO_REASON_OPTION_KEY);
        return;
    };
    if conn.entity.is_none() {
        conn.deny("unknown process or profile", NO_REASON_OPTION_KEY);
        return;
    }

    // Check if the layered profile needs updating.
    if profile.needs_update() {
        conn.profile_revision_counter = profile.update(|id| env.load_local_profile(id));
        conn.save_when_finished = true;
        tracing::info!("filter: profile updated, re-evaluating verdict of {conn}");
        if let Some(entity) = conn.entity.as_mut() {
            entity.reset_lists();
        }
    } else {
        let revision = profile.revision();
        if conn.profile_revision_counter != revision {
            conn.profile_revision_counter = revision;
            conn.save_when_finished = true;
        }
    }

    // Prepare the entity and resolve all filter list matches.
    if let Some(entity) = conn.entity.as_mut() {
        entity.resolve_sub_domain_lists(profile.filter_sub_domains());
        entity.enable_cname_check(profile.filter_cnames());
        entity.load_lists();
    }

    let (done, default_action) = run_deciders(&DEFAULT_DECIDERS, conn, profile, env);
    if done {
        return;
    }

    // DNS requests are always default allowed, as the endpoint lists could not
    // be checked fully.
    if conn.conn_type == ConnectionType::Dns {
        conn.accept("allowing dns request", NO_REASON_OPTION_KEY);
        return;
    }

    // Deciders did not conclude, use the default action.
    match default_action {
        DEFAULT_ACTION_PERMIT => {
            conn.accept("allowed by default action", CFG_OPTION_DEFAULT_ACTION_KEY);
        }
        DEFAULT_ACTION_ASK => {
            // Only prompt if there has not been a decision already. This
            // prevents prompts from being created when re-evaluating.
            if conn.verdict == Verdict::Undecided {
                env.prompt(conn);
            }
        }
        _ => conn.deny("blocked by default action", CFG_OPTION_DEFAULT_ACTION_KEY),
    }
}

/// Runs the deciders in order; returns whether one concluded, else the
/// default action of the profile.
pub fn run_deciders(
    deciders: &[Decider],
    conn: &mut Connection,
    profile: &LayeredProfile,
    env: &dyn FirewallEnv,
) -> (bool, u8) {
    for decider in deciders {
        if decider(conn, profile, env) {
            return (true, DEFAULT_ACTION_NOT_SET);
        }
    }
    (false, profile.default_action())
}
