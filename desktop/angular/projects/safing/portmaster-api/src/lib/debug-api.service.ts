import { HttpClient } from '@angular/common/http';
import { Inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { PORTMASTER_HTTP_API_ENDPOINT } from './portapi.service';

@Injectable({
  providedIn: 'root',
})
export class DebugAPI {
  constructor(
    private http: HttpClient,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpAPI: string,
  ) { }

  ping(): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/ping`, {
      responseType: 'text'
    })
  }

  ready(): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/ready`, {
      responseType: 'text'
    })
  }

  getStack(): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/debug/stack`, {
      responseType: 'text'
    })
  }

  getDebugInfo(style = 'github'): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/debug/info`, {
      params: {
        style,
      },
      responseType: 'text',
    })
  }

  getCoreDebugInfo(style = 'github'): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/debug/core`, {
      params: {
        style,
      },
      responseType: 'text',
    })
  }

  getProfileDebugInfo(source: string, id: string, style = 'github'): Observable<string> {
    return this.http.get(`${this.httpAPI}/v1/debug/network`, {
      params: {
        profile: `${source}/${id}`,
        style,
      },
      responseType: 'text',
    })
  }
}
