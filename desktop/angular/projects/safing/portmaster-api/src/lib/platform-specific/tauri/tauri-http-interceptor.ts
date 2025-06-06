import { HttpEvent, HttpHandlerFn, HttpRequest, HttpResponse, HttpHeaders, HttpErrorResponse } from '@angular/common/http';
import { from, Observable, switchMap, map, catchError, throwError } from 'rxjs';
import { invoke } from '@tauri-apps/api/core'

/**
 * TauriHttpInterceptor intercepts HTTP requests and routes them through Tauri's `@tauri-apps/plugin-http` API.
 * 
 * This allows HTTP requests to be executed from the Tauri application binary instead of the WebView, 
 * enabling more secure and direct communication with external APIs.
 * 
 * The interceptor handles various response types (e.g., JSON, text, blob, arraybuffer) and ensures
 * that headers and response data are properly mapped to Angular's HttpResponse format.
 * 
 * References:
 * - https://angular.dev/guide/http/interceptors 
 */
export function TauriHttpInterceptor(req: HttpRequest<unknown>, next: HttpHandlerFn): Observable<HttpEvent<unknown>> {
    const fetchOptions: RequestInit = {
        method: req.method,
        headers: req.headers.keys().reduce((acc: Record<string, string>, key) => {
            acc[key] = req.headers.get(key) || '';
            return acc;
        }, {}),
        body: getRequestBody(req),
    };
    //console.log('[TauriHttpInterceptor] Fetching:', req.url, "Headers:", fetchOptions.headers);
    return from(send_tauri_http_request(req.url, fetchOptions)).pipe(
        switchMap(response => {
            // Copy all response headers
            const headerMap: Record<string, string> = {};                
            response.headers.forEach((value: string, key: string) => {
                headerMap[key] = value;
            });                
            const headers = new HttpHeaders(headerMap);

            // Check if response status is ok (2xx)
            if (!response.ok) {
                // Get the error content
                return from(response.text()).pipe(
                    map(errorText => {
                        throw new HttpErrorResponse({
                            error: errorText,
                            headers: headers,
                            status: response.status,
                            statusText: response.statusText,
                            url: req.url
                        });
                    })
                );
            }
            
            // Get the response type from the request
            const responseType = req.responseType || 'json';
                
            // Helper function to create HttpResponse from body
            const createResponse = (body: any): HttpEvent<unknown> => {
                return new HttpResponse({
                    body,
                    status: response.status,
                    headers: headers,
                    url: req.url
                }) as HttpEvent<unknown>;
            };
                
            switch (responseType) {
                case 'text':
                    return from(response.text()).pipe(map(createResponse));
                case 'arraybuffer':
                    return from(response.arrayBuffer()).pipe(map(createResponse));
                case 'blob':
                    return from(response.blob()).pipe(
                        map(blob => {
                            const contentType = response.headers.get('content-type') || '';
                            // Create a new blob with the proper MIME type
                            if (contentType && (!blob.type || blob.type === 'application/octet-stream')) {
                                const typedBlob = new Blob([blob], { type: contentType });
                                return createResponse(typedBlob);
                            }
                            
                            return createResponse(blob);
                        })
                    );
                case 'json':
                default:
                    return from(response.text()).pipe(
                        map(body => {
                            let parsedBody: any;
                            try {
                                // Only attempt to parse as JSON if we have content
                                // and either explicitly requested JSON or content-type is JSON
                                if (body && (responseType === 'json' || 
                                    (response.headers.get('content-type') || '').includes('application/json'))) {
                                    parsedBody = JSON.parse(body);
                                } else {
                                    parsedBody = body;
                                }
                            } catch (e) {
                                console.warn('[TauriHttpInterceptor] Failed to parse JSON response:', e);
                                parsedBody = body;
                            }
                            return createResponse(parsedBody);
                        })
                    );
            }
        }),
        catchError(error => {
            console.error('[TauriHttpInterceptor] Request failed:', error);
            
            // If it's already an HttpErrorResponse, just return it
            if (error instanceof HttpErrorResponse) {
                return throwError(() => error);
            }
            
            // Otherwise create a new HttpErrorResponse with available information
            return throwError(() => new HttpErrorResponse({
                error: error.message || 'Unknown error occurred',
                status: error.status || 0,
                statusText: error.statusText || 'Unknown Error',
                url: req.url,
                headers: error.headers ? new HttpHeaders(error.headers) : new HttpHeaders()
            }));
        })
    );
}

function getRequestBody(req: HttpRequest<unknown>): any {
    if (!req.body) {
        return undefined;
    }
    
    // Handle different body types properly
    if (req.body instanceof FormData || 
        req.body instanceof Blob || 
        req.body instanceof ArrayBuffer ||
        req.body instanceof URLSearchParams) {
        return req.body;
    }
    
    // Default to JSON stringify for object data
    return JSON.stringify(req.body);
}

export async function send_tauri_http_request(
    url: string,
    init: RequestInit = {}
  ): Promise<Response> {
    // Extract method, headers, and body buffer
    const method = init.method || 'GET';
    const headers = [...(init.headers instanceof Headers
      ? (() => {
          const headerArray: [string, string][] = [];
          init.headers.forEach((value, key) => headerArray.push([key, value]));
          return headerArray;
        })()
      : Object.entries(init.headers || {}))];

    let body: Uint8Array | undefined;
    if (init.body) {
        if (typeof init.body === 'string') {
            // Most efficient way to convert a string to Uint8Array
            body = new TextEncoder().encode(init.body);
        } else if (init.body instanceof ArrayBuffer) {
            body = new Uint8Array(init.body);
        } else if (init.body instanceof Uint8Array) {
            body = init.body;
        } else if (init.body instanceof Blob) {
            // Efficiently read Blob data
            body = new Uint8Array(await init.body.arrayBuffer());
        } else if (init.body instanceof URLSearchParams) {
            body = new TextEncoder().encode(init.body.toString());
        } else {
            // Fallback for other types, though the inefficient path is kept for unsupported types
            // This path should ideally be avoided by handling types in getRequestBody.
            console.warn('[TauriHttpInterceptor] Using inefficient body conversion for unknown type.');
            body = new Uint8Array(await new Response(init.body as any).arrayBuffer());
        }
    }
  
    const res = await invoke<{
      status: number;
      status_text: string;
      headers: [string, string][];
      body: number[];
    }>('send_tauri_http_request', { url, opts: { method, headers, body: body ? Array.from(body) : undefined } });
                                  
    return new Response(new Uint8Array(res.body), {
      status: res.status,
      statusText: res.status_text,
      headers: res.headers,
    });
  }