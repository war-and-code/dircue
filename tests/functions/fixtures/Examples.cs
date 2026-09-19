class Examples {
    static int Defective(int divisor) {
        return 10 / divisor;
    }
    static int Guarded(int divisor) {
        if (divisor == 0) return 0;
        return 10 / divisor;
    }
    interface Operation { int Apply(int value); }
    static int Polymorphic(Operation operation, int value) {
        return operation.Apply(value);
    }
    static int Callback(System.Func<int, int> operation, int value) {
        return operation(value);
    }
    static int Table(int index) {
        return new int[] {10, 20, 30}[index];
    }
    static int Recover(string value) {
        try { return int.Parse(value); }
        catch (System.FormatException) { return 0; }
    }
}
