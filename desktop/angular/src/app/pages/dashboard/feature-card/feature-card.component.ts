import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input, OnChanges, OnDestroy, SimpleChanges, inject } from '@angular/core';
import { Router } from '@angular/router';
import { BoolSetting, ConfigService, Feature } from '@safing/portmaster-api';
import { Subscription } from 'rxjs';
import { INTEGRATION_SERVICE } from 'src/app/integration';

@Component({
  selector: 'app-feature-card',
  templateUrl: './feature-card.component.html',
  styleUrls: ['./feature-card.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class FeatureCardComponent implements OnChanges, OnDestroy {
  private readonly cdr = inject(ChangeDetectorRef);
  private readonly configService = inject(ConfigService);
  private readonly router = inject(Router);
  private readonly integration = inject(INTEGRATION_SERVICE);

  private configValueSubscription = Subscription.EMPTY;

  @Input()
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v)
  }
  get disabled() { return this._disabled }
  _disabled = false;

  get comingSoon() { return this.feature?.ComingSoon || false }

  @Input()
  feature?: Feature;

  planColor: string | null = null;

  configValue: boolean | undefined = undefined;

  ngOnChanges(changes: SimpleChanges): void {
    if ('feature' in changes) {
      this.configValueSubscription.unsubscribe();
      this.configValueSubscription = Subscription.EMPTY;

      if (!!this.feature?.ConfigKey) {
        this.configValueSubscription =
          this.configService.watch<BoolSetting>(this.feature!.ConfigKey)
            .subscribe(value => {
              this.configValue = value;
              this.cdr.markForCheck();
            });
      }

      if (this.feature?.InPackage?.HexColor) {
        this.planColor = getContrastFontColor(this.feature.InPackage.HexColor);
        // console.log(this.feature.InPackage.HexColor, this.planColor)
        this.cdr.markForCheck();
      }
    }
  }

  ngOnDestroy() {
    this.configValueSubscription.unsubscribe();
  }

  updateSettingsValue(newValue: boolean) {
    this.configService.save(this.feature!.ConfigKey, newValue)
      .subscribe()
  }

  navigateToConfigScope() {
    if (this.disabled) {
      this.integration.openExternal("https://safing.io/pricing?source=Portmaster")
      return;
    }

    let key: string | undefined;
    if (this.feature?.ConfigScope) {
      key = 'config:' + this.feature?.ConfigScope;
    } else {
      key = this.feature?.ConfigKey;
    }

    if (!key) {
      return
    }


    this.router.navigate(['/settings'], {
      queryParams: {
        setting: key,
      }
    })
  }
}

function parseColor(input: string): number[] {
  if (input.substr(0, 1) === '#') {
    const collen = (input.length - 1) / 3;
    const fact = [17, 1, 0.062272][collen - 1];
    return [
      Math.round(parseInt(input.substr(1, collen), 16) * fact),
      Math.round(parseInt(input.substr(1 + collen, collen), 16) * fact),
      Math.round(parseInt(input.substr(1 + 2 * collen, collen), 16) * fact),
    ];
  }

  return input
    .split('(')[1]
    .split(')')[0]
    .split(',')
    .map((x) => +x);
}

function getContrastFontColor(bgColor: string): string {
  // if (red*0.299 + green*0.587 + blue*0.114) > 186 use #000000 else use #ffffff
  // based on https://stackoverflow.com/a/3943023

  let col = bgColor;
  if (bgColor.startsWith('#') && bgColor.length > 7) {
    col = bgColor.slice(0, 7);
  }
  const [r, g, b] = parseColor(col);

  if (r * 0.299 + g * 0.587 + b * 0.114 > 186) {
    return '#000000';
  }

  return '#ffffff';
}
