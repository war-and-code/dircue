package example.geometry;

import java.util.List;
import java.util.function.IntUnaryOperator;

interface Measurable {
    int area();
    default boolean positive() { return area() > 0; }
}

record Point(int x, int y) {}

public final class Shapes {
    private final List<Point> points;

    public Shapes(List<Point> points) { this.points = points; }

    public int score(int limit) {
        IntUnaryOperator twice = value -> value * 2;
        int total = 0;
        for (Point point : points) {
            if (point.x() > limit && point.y() != 0) {
                total += twice.applyAsInt(point.y());
            } else {
                total--;
            }
        }
        return switch (total) {
            case 0 -> 1;
            default -> total;
        };
    }

    static final class Square implements Measurable {
        private final int side;
        Square(int side) { this.side = side; }
        @Override public int area() { return side * side; }
    }
}
