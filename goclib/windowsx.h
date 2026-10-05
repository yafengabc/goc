#ifndef GOC_WINDOWSX_H
#define GOC_WINDOWSX_H

#include <windef.h>
#include <winuser.h>

/* goc windowsx.h -- the handful of helper macros GUI code typically reaches
 * for. The word-packing macros themselves live in windef.h because that is
 * where the real SDK defines them; this header adds the coordinate/mouse
 * helpers that depend on them. */

#define GET_X_LPARAM(lp)  ((int)(short)LOWORD(lp))
#define GET_Y_LPARAM(lp)  ((int)(short)HIWORD(lp))
#define GET_XPARAM(param)  ((int)(short)LOWORD(param))

#endif /* GOC_WINDOWSX_H */
