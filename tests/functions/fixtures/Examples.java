class Examples {
    static int defective(int divisor) {
        return 10 / divisor;
    }
    static int guarded(int divisor) {
        if (divisor == 0) return 0;
        return 10 / divisor;
    }
    interface Operation { int apply(int value); }
    static int polymorphic(Operation operation, int value) {
        return operation.apply(value);
    }
    static int callback(java.util.function.IntUnaryOperator operation, int value) {
        return operation.applyAsInt(value);
    }
    static int table(int index) {
        return new int[] {10, 20, 30}[index];
    }
    static int recover(String value) {
        try { return Integer.parseInt(value); }
        catch (NumberFormatException error) { return 0; }
    }
}
