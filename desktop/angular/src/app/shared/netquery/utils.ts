import { Condition, Matcher } from "@safing/portmaster-api";
import { objKeys } from "../utils";

export const connectionFieldTranslation: { [key: string]: string } = {
  domain: "域名",
  profile: "应用",
  path: '程序路径',
  scope: '范围',
  as_owner: "提供商",
  country: "国家/地区",
  direction: '方向',
  started: '开始时间',
  ended: '结束时间',
  remote_ip: '远程 IP',
  verdict: '判定',
  encrypted: '已加密',
  internal: '内部',
  asn: 'ASN',
  tunneled: 'SPN 活动',
  active: '活动',
  allowed: '已允许',
  from: '起始',
  to: '截止',
  remote_port: '端口',
  bytes_sent: '已发送字节',
  bytes_received: '已接收字节'
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
