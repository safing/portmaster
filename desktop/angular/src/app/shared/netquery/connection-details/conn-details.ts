import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input, OnChanges, OnDestroy, OnInit, SimpleChanges, inject } from "@angular/core";
import { BandwidthChartResult, ConnectionBandwidthChartResult, IPProtocol, IPScope, IsDenied, IsDNSRequest, Netquery, NetqueryConnection, PortapiService, Process, Verdict, VerdictTranslation } from "@safing/portmaster-api";
import { SfngDialogService } from '@safing/ui';
import { Subscription } from "rxjs";
import { ProcessDetailsDialogComponent } from '../../process-details-dialog';
import { NetqueryHelper } from "../connection-helper.service";
import { BytesPipe } from "../../pipes/bytes.pipe";
import { formatDuration } from "../../pipes";



@Component({
  selector: 'sfng-netquery-conn-details',
  styleUrls: ['./conn-details.scss'],
  templateUrl: './conn-details.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngNetqueryConnectionDetailsComponent implements OnInit, OnDestroy, OnChanges {
  helper = inject(NetqueryHelper)
  private readonly portapi = inject(PortapiService)
  private readonly dialog = inject(SfngDialogService)
  private readonly cdr = inject(ChangeDetectorRef)
  private readonly netquery = inject(Netquery)

  @Input()
  conn: NetqueryConnection | null = null;

  process: Process | null = null;

  readonly IsDNS = IsDNSRequest;
  readonly verdict = VerdictTranslation;
  readonly Protocols = IPProtocol;
  readonly scopes = IPScope;
  private _subscription = Subscription.EMPTY;

  formatBytes = (n: d3.NumberValue, seriesKey?: string) => {
    let prefix = '';
    if (seriesKey !== undefined) {
      prefix = seriesKey === 'incoming' ? '已接收：' : '已发送：'
    }
    return prefix + new BytesPipe().transform(n.valueOf())
  }

  formatTime = (n: Date) => {
    const diff = Math.floor(new Date().getTime() - n.getTime())
    return formatDuration(diff, false, true) + "前"
  }

  tooltipFormat = (n: BandwidthChartResult<any>) => {
    const bytes = new BytesPipe().transform
    const received = `已接收：${bytes(n?.incoming || 0)}`;
    const sent = `已发送：${bytes(n?.outgoing || 0)}`

    if ((n?.incoming || 0) > (n?.outgoing || 0)) {
      return `${received}\n${sent}`
    }
    return `${sent}\n${received}`
  }

  connectionNotice: string = '';
  bwData: ConnectionBandwidthChartResult[] = [];

  ngOnChanges(changes: SimpleChanges) {
    if (!!changes?.conn) {
      this.updateConnectionNotice();
      this.loadBandwidthChart();

      if (this.conn?.extra_data?.pid !== undefined) {
        this.portapi.get<Process>(`network:tree/${this.conn.extra_data.pid}-${this.conn.extra_data.processCreatedAt}`)
          .subscribe({
            next: p => {
              this.process = p;
              this.cdr.markForCheck();
            },
            error: () => {
              this.process = null; // the process does not exist anymore
              this.cdr.markForCheck();
            }
          })
      } else {
        this.process = null;
      }
    }
  }

  ngOnInit() {
    this._subscription = this.helper.refresh.subscribe(() => {
      this.updateConnectionNotice();
      this.loadBandwidthChart();

      this.cdr.markForCheck();
    })
  }

  ngOnDestroy() {
    this._subscription.unsubscribe();
  }

  openProcessDetails() {
    this.dialog.create(ProcessDetailsDialogComponent, {
      data: this.process,
      backdrop: true,
      autoclose: true,
    })
  }

  private loadBandwidthChart() {
    this.bwData = [];

    if (!this.conn) {
      this.cdr.markForCheck()

      return;
    }

    this.netquery.connectionBandwidthChart([this.conn!.id], 1)
      .subscribe(result => {
        if (!result[this.conn!.id]?.length) {
          return;
        }

        this.bwData = result[this.conn!.id];

        this.cdr.markForCheck();
      });
  }

  private updateConnectionNotice() {
    this.connectionNotice = '';
    if (!this.conn) {
      return;
    }

    if (this.conn!.verdict === Verdict.Failed) {
      this.connectionNotice = '在先前的设置下失败。'
      return;
    }

    if (IsDenied(this.conn!.verdict)) {
      this.connectionNotice = '已被先前的设置阻止。';
    } else {
      this.connectionNotice = '已被先前的设置允许。';
    }

    this.connectionNotice += '您当前的设置可能会做出不同的判定。'
  }
}
