int main(void) {
    const char *p = "hi from gocl/linux\n";
    write(1, p, 19);
    return 0;
}
