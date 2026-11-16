import { Condition, Matcher } from "@safing/portmaster-api";
import { I18nService } from "src/i18n";
import { objKeys } from "../utils";

export const connectionFieldTranslation: { [key: string]: string } = {
  domain: "Domain",
  profile: "App",
  path: 'Binary Path',
  scope: 'Scope',
  as_owner: "Provider",
  country: "Country",
  direction: 'Direction',
  started: 'Started',
  ended: 'Ended',
  remote_ip: 'Remote IP',
  verdict: 'Verdict',
  encrypted: 'Encrypted',
  internal: 'Internal',
  asn: 'ASN',
  tunneled: 'SPN Active',
  active: 'Active',
  allowed: 'Allowed',
  from: 'From',
  to: 'To',
  remote_port: 'Port',
  bytes_sent: 'Bytes Sent',
  bytes_received: 'Bytes Received'
}

/** Build a fresh field-label map for the currently selected UI language. */
export function createLocalizedFieldTranslation(i18n: I18nService): { [key: string]: string } {
  const result: { [key: string]: string } = {};
  objKeys(connectionFieldTranslation).forEach(key => {
    const translated = i18n.t(`lc.field.${key}`);
    result[key] = translated === `lc.field.${key}`
      ? connectionFieldTranslation[key]
      : translated;
  });
  return result;
}

export function isMatcher(v: any | Matcher): v is Matcher {
  return typeof v === 'object' && ('$eq' in v || '$ne' in v || '$like' in v || '$in' in v || '$notin' in v);
}

export function mergeConditions(cond1: Condition, cond2: Condition): Condition {
  const result: Condition = {};

  objKeys(cond1).forEach(key => {
    let val = cond1[key];
    if (Array.isArray(val)) {
      result[key] = val;
    } else {
      result[key] = [val];
    }
  })

  objKeys(cond2).forEach(key => {
    let val = cond2[key];
    if (!Array.isArray(val)) {
      val = [val]
    }

    if (!(key in result)) {
      result[key] = val;
    } else {
      result[key] = [
        ...(result[key] as any), // this must be an array here
        ...val,
      ]
    }
  })


  return result;
}
