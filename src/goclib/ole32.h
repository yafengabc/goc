#ifndef GOC_OLE32_H
#define GOC_OLE32_H

#include <windef.h>

/* goc ole32.h -- the COM entry points GUI code actually calls. Full COM
 * (IUnknown/IID/CLSID plumbing, QueryInterface/AddRef marshalling) is far
 * beyond what a plain C GUI needs; MDQuickViewer only initialises the
 * apartment and frees PIDLs allocated by the shell browse dialog. */

#define COINIT_APARTMENTTHREADED 0x2
#define COINIT_MULTITHREADED     0x0
#define COINIT_DISABLE_OLE1DDE  0x4

extern HRESULT CoInitializeEx(LPVOID pvReserved, DWORD dwCoInit), ole32;
extern void    CoUninitialize(void), ole32;
extern void    CoTaskMemFree(LPVOID pv), ole32;

#endif /* GOC_OLE32_H */
