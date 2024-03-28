
// the following settings are stronger than rules
// and cannot be "fixed" by creating a new allow/deny

import { Pipe, PipeTransform } from "@angular/core";
import { IsDenied, NetqueryConnection } from "@safing/portmaster-api";

// rule.
let optionKeys = new Set([
  "filter/blockInternet",
  "filter/blockLAN",
  "filter/blockLocal",
  "filter/blockP2P",
  "filter/blockInbound"
])

@Pipe({
  name: "canUseRules",
  pure: true,
})
export class CanUseRulesPipe implements PipeTransform {
  transform(conn: NetqueryConnection): boolean {
    if (!conn) {
      return false;
    }
    if (!!conn.extra_data?.reason?.OptionKey && IsDenied(conn.verdict)) {
      return !optionKeys.has(conn.extra_data.reason.OptionKey);
    }
    return true;
  }
}

