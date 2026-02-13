#include "core.h"
#include "utils.h"
#include <stdio.h>
#include <chrono>

int main() {
    printf("Running benchmarks...\n");
    
    auto start = std::chrono::high_resolution_clock::now();
    
    core_init();
    
    for (int i = 0; i < 1000000; i++) {
        core_process(i);
        utils_add(i, i + 1);
    }
    
    auto end = std::chrono::high_resolution_clock::now();
    auto duration = std::chrono::duration_cast<std::chrono::microseconds>(end - start);
    
    printf("Benchmark completed in %ld microseconds\n", duration.count());
    
    return 0;
}
