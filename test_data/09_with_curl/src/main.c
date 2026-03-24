#include <stdio.h>
#include <curl/curl.h>

int main(void) {
    printf("curl version: %s\n", curl_version());

    CURL *curl = curl_easy_init();
    if (curl) {
        curl_easy_setopt(curl, CURLOPT_URL, "http://www.baidu.com");
        curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, NULL);

        printf("Performing HTTP GET request...\n");
        CURLcode res = curl_easy_perform(curl);

        if (res != CURLE_OK) {
            fprintf(stderr, "curl_easy_perform() failed: %s\n",
                    curl_easy_strerror(res));
        } else {
            printf("Request completed successfully\n");
        }

        curl_easy_cleanup(curl);
    }

    return 0;
}
