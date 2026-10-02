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
        return "任播"
      }
      return conn.country;
    }

    const scope = conn.scope;

    if (IsGlobalScope(scope)) {
      return '互联网'
    }

    if (IsLANScope(scope)) {
      return '局域网';
    }

    if (IsLocalhost(scope)) {
      return '本机'
    }

    return '';
  }
}
