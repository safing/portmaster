import { Component, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { BoolSetting, ConfigService, Database, FeatureID, Netquery, SPNService } from '@safing/portmaster-api';
import { Subject, interval, map, merge, repeat } from 'rxjs';
import { SessionDataService } from 'src/app/services';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { fadeInAnimation, moveInOutListAnimation } from 'src/app/shared/animations';

@Component({
  templateUrl: './monitor.html',
  styleUrls: ['../page.scss', './monitor.scss'],
  providers: [],
  animations: [fadeInAnimation, moveInOutListAnimation],
})
export class MonitorPageComponent {
  session = inject(SessionDataService);
  netquery = inject(Netquery);
  reload = new Subject<void>();

  configService = inject(ConfigService);
  uai = inject(ActionIndicatorService);

  historyEnabled = inject(ConfigService)
    .watch<BoolSetting>('history/enable');

  canUseHistory = inject(SPNService).profile$
    .pipe(
      map(profile => {
        return profile?.current_plan?.feature_ids?.includes(FeatureID.History) || false;
      })
    );

  history = inject(Netquery)
    .query({
      select: [
        {
          $min: {
            field: "started",
            as: "first_connection",
          },
        },
        {
          $count: {
            field: "*",
            as: "totalCount"
          }
        }
      ],
      databases: [Database.History]
    }, 'monitor-get-first-history-connection')
    .pipe(
      repeat({ delay: () => merge(interval(10000), this.reload) }),
      map(result => {
        if (!result.length || result[0].totalCount === 0) {
          return null
        }

        return {
          first: new Date(result[0].first_connection),
          count: result[0].totalCount,
        }
      }),
      takeUntilDestroyed()
    );

  enableHistory() {
    this.configService.save('history/enable', true)
      .subscribe();
  }

  clearHistoryData() {
    this.netquery.cleanProfileHistory([])
      .subscribe(() => {
        this.reload.next();
      })
  }
}
