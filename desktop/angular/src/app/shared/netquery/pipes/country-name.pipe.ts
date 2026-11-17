import { HttpClient } from '@angular/common/http';
import { Pipe, PipeTransform, Injectable, inject } from '@angular/core';
import { GeoCoordinates, SPNService } from '@safing/portmaster-api';
import { Language, LocalizedValueService } from 'src/i18n';
import { environment } from 'src/environments/environment';
import { ActionIndicatorService } from '../../action-indicator';
import { objKeys } from '../../utils';

export interface CountryListResponse {
  [countryKey: string]: {
    Code: string;
    Name: string;
    Center: GeoCoordinates;
    Continent: {
      Code: string;
      Region: string;
      Name: string;
    }
  }
}

@Injectable()
export class CountryNameService {
  private readonly spn = inject(SPNService);
  private readonly http = inject(HttpClient);
  private readonly uai = inject(ActionIndicatorService);
  private readonly localized = inject(LocalizedValueService);

  private map: Map<string, string> = new Map();

  constructor() {
    this.http.get<CountryListResponse>(`${environment.httpAPI}/v1/intel/geoip/countries`)
      .subscribe({
        next: response => {
          objKeys(response)
            .forEach(key => {
              this.map.set(key as string, response[key].Name);
            });
        },
        error: err => {
          this.uai.error('Failed to fetch country data', this.uai.getErrorMessage(err));
        }
      })
  }

  resolveName(code: string): string {
    // Backend delivers English names; localize by ISO code with a safe
    // fallback to the original value for unknown codes.
    return this.localized.countryName(code, this.map.get(code) || '');
  }
}

@Pipe({
  name: 'countryName',
})
export class CountryNamePipe implements PipeTransform {
  private countryService = inject(CountryNameService);

  // `version` comes from the i18nVersion pipe and makes this pure pipe
  // re-resolve the name when the UI language changes.
  transform(countryCode: string, _version?: Language) {
    return this.countryService.resolveName(countryCode);
  }
}
