import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, ElementRef, OnInit, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { ConfigService, Setting } from "@safing/portmaster-api";
import { Step } from "@safing/ui";
import { of } from "rxjs";
import { mergeMap } from "rxjs/operators";
import { SaveSettingEvent } from "src/app/shared/config/generic-setting";

@Component({
  templateUrl: './step-2-trackers.html',
  styleUrls: ['../step.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Step2TrackersComponent implements Step, OnInit {
  private destroyRef = inject(DestroyRef);

  validChange = of(true)

  setting: Setting | null = null;

  constructor(
    public configService: ConfigService,
    public readonly elementRef: ElementRef,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    this.configService.get('filter/lists')
      .pipe(
        mergeMap(setting => {
          this.setting = setting;

          return this.configService.watch(setting.Key)
        }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(value => {
        this.setting!.Value = value;

        this.cdr.markForCheck();
      });
  }

  saveSetting(event: SaveSettingEvent) {
    this.configService.save(event.key, event.value)
      .subscribe()
  }
}
