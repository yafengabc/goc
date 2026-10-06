/* <sys/stat.h> -- POSIX spelling of goclib's <stat.h>.
 *
 * goclib keeps the file metadata header at goclib/stat.h, and its own
 * preprocessor resolves <sys/stat.h> to it by basename (common/preprocess.go),
 * so a goc-compiled program can write the POSIX spelling it would use
 * anywhere else. A host compiler has no such rule: -I goclib makes
 * <sys/stat.h> look for goclib/sys/stat.h and find nothing.
 *
 * This file is that path. It exists so `#include <sys/stat.h>' works when
 * goclib is compiled by gcc/clang as an ordinary library, without goclib's own
 * sources having to spell the header differently on the two hosts -- the same
 * source file compiles unchanged under both. It carries no declarations of its
 * own; the guard in stat.h keeps a double inclusion harmless either way.
 */
#include "../stat.h"