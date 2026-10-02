#include <stdio.h>

/* C23 preprocessor operators __has_include / __has_c_attribute */
#if __has_include(<stdio.h>)
#define HAVE_STDIO 1
#else
#define HAVE_STDIO 0
#endif

#if __has_include("this_file_does_not_exist_xyz.h")
#define HAVE_MISSING 1
#else
#define HAVE_MISSING 0
#endif

#if __has_c_attribute(deprecated)
#define ATTR_SUPPORTED 202311
#else
#define ATTR_SUPPORTED 0
#endif

#if __has_c_attribute(not_a_real_attribute_zzz)
#define ATTR_UNKNOWN 1
#else
#define ATTR_UNKNOWN 0
#endif

int main(void) {
    printf("HAVE_STDIO=%d\n", HAVE_STDIO);
    printf("HAVE_MISSING=%d\n", HAVE_MISSING);
    printf("ATTR_SUPPORTED=%d\n", ATTR_SUPPORTED);
    printf("ATTR_UNKNOWN=%d\n", ATTR_UNKNOWN);
    return 0;
}
