import { Pipe, PipeTransform } from "@angular/core";
import { ExpertiseLevel, NetqueryConnection } from "@safing/portmaster-api";


@Pipe({
  name: "canShowConnection",
  pure: true,
})
export class CanShowConnection implements PipeTransform {
  transform(conn: NetqueryConnection, level: ExpertiseLevel) {
    if (!conn) {
      return false;
    }
    if (level === ExpertiseLevel.Developer) {
      // we show all connections for developers
      return true;
    }
    // if we are in advanced or simple mode we should
    // hide internal connections.
    return !conn.internal;
  }
}
