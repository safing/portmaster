import { Pipe, PipeTransform } from '@angular/core';
import { IsGlobalScope, IsLANScope, IsLocalhost, NetqueryConnection } from '@safing/portmaster-api';

@Pipe({
  name: 'connectionLocation',
  pure: true,
})
export class ConnectionLocationPipe implements PipeTransform {
  transform(conn: NetqueryConnection): string {
    if (conn.type === 'dns') {
      return '';
    }
    if (!!conn.country) {
      if (conn.country === "__") {
        return "Anycast"
      }
      return conn.country;
    }

    const scope = conn.scope;

    if (IsGlobalScope(scope)) {
      return 'Internet'
    }

    if (IsLANScope(scope)) {
      return 'LAN';
    }

    if (IsLocalhost(scope)) {
      return 'Device'
    }

    return '';
  }
}
