using System;
using System.Collections.Generic;
using System.Linq;

namespace Example.Geometry;

public interface IMeasurable
{
    int Area();
}

public record Point(int X, int Y);

public sealed class Shapes
{
    private readonly List<Point> points;

    public Shapes(List<Point> points) => this.points = points;

    public int Score(int limit)
    {
        Func<int, int> twice = value => value * 2;
        int total = 0;
        foreach (Point point in points)
        {
            if (point.X > limit && point.Y != 0)
                total += twice(point.Y);
            else
                total--;
        }
        return total switch { 0 => 1, _ => total };
    }

    public sealed class Square : IMeasurable
    {
        public int Side { get; init; }
        public int Area() => Side * Side;
        public IEnumerable<int> Sides() => Enumerable.Range(0, Side).Select(x => x * 2);
    }
}
