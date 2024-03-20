import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { Inject, Injectable, Optional } from '@angular/core';
import { Observable, of, throwError } from 'rxjs';
import { catchError, map } from 'rxjs/operators';
import { PORTMASTER_HTTP_API_ENDPOINT } from './portapi.service';

export interface MetaEndpointParameter {
  Method: string;
  Field: string;
  Value: string;
  Description: string;
}

export interface MetaEndpoint {
  Path: string;
  MimeType: string;
  Read: number;
  Write: number;
  Name: string;
  Description: string;
  Parameters: MetaEndpointParameter[];
}

export interface AuthPermission {
  Read: number;
  Write: number;
  ReadRole: string;
  WriteRole: string;
}

export interface MyProfileResponse {
  profile: string;
  source: string;
  name: string;
}

export interface AuthKeyResponse {
  key: string;
  validUntil: string;
}

@Injectable()
export class MetaAPI {
  constructor(
    private http: HttpClient,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) @Optional() private httpEndpoint: string = 'http://localhost:817/api',
  ) { }

  listEndpoints(): Observable<MetaEndpoint[]> {
    return this.http.get<MetaEndpoint[]>(`${this.httpEndpoint}/v1/endpoints`)
  }

  permissions(): Observable<AuthPermission> {
    return this.http.get<AuthPermission>(`${this.httpEndpoint}/v1/auth/permissions`)
  }

  myProfile(): Observable<MyProfileResponse> {
    return this.http.get<MyProfileResponse>(`${this.httpEndpoint}/v1/app/profile`)
  }

  requestApplicationAccess(appName: string, read: 'user' | 'admin' = 'user', write: 'user' | 'admin' = 'user'): Observable<AuthKeyResponse> {
    let params = new HttpParams()
      .set("app-name", appName)
      .set("read", read)
      .set("write", write)

    return this.http.get<AuthKeyResponse>(`${this.httpEndpoint}/v1/app/auth`, { params })
  }

  login(bearer: string): Observable<boolean>;
  login(username: string, password: string): Observable<boolean>;
  login(usernameOrBearer: string, password?: string): Observable<boolean> {
    let login: Observable<void>;

    if (!!password) {
      login = this.http.get<void>(`${this.httpEndpoint}/v1/auth/basic`, {
        headers: {
          'Authorization': `Basic ${btoa(usernameOrBearer + ":" + password)}`
        }
      })
    } else {
      login = this.http.get<void>(`${this.httpEndpoint}/v1/auth/bearer`, {
        headers: {
          'Authorization': `Bearer ${usernameOrBearer}`
        }
      })
    }

    return login.pipe(
      map(() => true),
      catchError(err => {
        if (err instanceof HttpErrorResponse) {
          if (err.status === 401) {
            return of(false);
          }
        }

        return throwError(() => err)
      })
    )
  }

  logout(): Observable<void> {
    return this.http.get<void>(`${this.httpEndpoint}/v1/auth/reset`);
  }
}
