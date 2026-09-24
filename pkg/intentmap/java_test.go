package intentmap

import (
	"testing"
)

func TestParseJavaSpringBootAnnotationWithMain(t *testing.T) {
	// The canonical Spring Boot entry point: @SpringBootApplication on a class
	// with a public static void main method.
	content := `package com.example.demo;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication
public class DemoApplication {

	public static void main(String[] args) {
		SpringApplication.run(DemoApplication.class, args);
	}

}`
	obs := parseJavaSpringBoot("src/main/java/com/example/demo/DemoApplication.java", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d: %v", len(obs), obs)
	}
	if obs[0].Name != "DemoApplication" {
		t.Errorf("Name = %q; want %q", obs[0].Name, "DemoApplication")
	}
	if obs[0].Properties["interface_kind"] != "spring_boot_application" {
		t.Errorf("interface_kind = %q; want spring_boot_application", obs[0].Properties["interface_kind"])
	}
	if obs[0].Basis != "code_syntax" {
		t.Errorf("Basis = %q; want code_syntax", obs[0].Basis)
	}
}

func TestParseJavaSpringBootAnnotationWithAttributesIsRecognised(t *testing.T) {
	// @SpringBootApplication(exclude={…}) is a valid form.
	content := `@SpringBootApplication(exclude = { DataSourceAutoConfiguration.class })
public class TestApp {
	public static void main(String[] args) {}
}`
	obs := parseJavaSpringBoot("src/main/java/TestApp.java", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d", len(obs))
	}
	if obs[0].Name != "TestApp" {
		t.Errorf("Name = %q; want TestApp", obs[0].Name)
	}
}

func TestParseJavaSpringBootSkipsTestSources(t *testing.T) {
	// Test source files annotated with @SpringBootApplication must not produce
	// false-positive entry points.
	content := `@SpringBootApplication
public class TestApplication {
	public static void main(String[] args) {}
}`
	obs := parseJavaSpringBoot("src/test/java/com/example/TestApplication.java", []byte(content))
	if len(obs) != 0 {
		t.Fatalf("test source should be skipped; got %v", obs)
	}
}

func TestParseJavaSpringBootAnnotationWithoutMainIsSkipped(t *testing.T) {
	// A class annotated with @SpringBootApplication but without a main method
	// is not a true entry point (could be a test, a custom configuration, etc.).
	content := `@SpringBootApplication
public class SomeConfig {
	// no main method
}`
	obs := parseJavaSpringBoot("src/main/java/SomeConfig.java", []byte(content))
	if len(obs) != 0 {
		t.Fatalf("class without main method must not produce an entry point; got %v", obs)
	}
}

func TestParseKotlinSpringBootMain(t *testing.T) {
	content := `package com.example

import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication

@SpringBootApplication
class ExampleApplication

fun main(args: Array<String>) {
	runApplication<ExampleApplication>(*args)
}
`
	obs := parseJavaSpringBoot("src/main/kotlin/com/example/ExampleApplication.kt", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 Kotlin observation, got %d: %v", len(obs), obs)
	}
}

func TestParseMavenMainClass(t *testing.T) {
	content := `<project>
  <build>
    <plugins>
      <plugin>
        <groupId>org.springframework.boot</groupId>
        <artifactId>spring-boot-maven-plugin</artifactId>
        <configuration>
          <mainClass>com.example.demo.DemoApplication</mainClass>
        </configuration>
      </plugin>
    </plugins>
  </build>
</project>`
	obs := parseMavenMainClass("pom.xml", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d: %v", len(obs), obs)
	}
	if obs[0].Name != "com.example.demo.DemoApplication" {
		t.Errorf("Name = %q; want com.example.demo.DemoApplication", obs[0].Name)
	}
	if obs[0].Properties["interface_kind"] != "maven_main_class" {
		t.Errorf("interface_kind = %q", obs[0].Properties["interface_kind"])
	}
}

func TestParseMavenMainClassNone(t *testing.T) {
	content := `<project><build></build></project>`
	obs := parseMavenMainClass("pom.xml", []byte(content))
	if len(obs) != 0 {
		t.Fatalf("unexpected observations: %v", obs)
	}
}

func TestParseGradleMainClass(t *testing.T) {
	content := `plugins {
	id 'org.springframework.boot' version '3.2.0'
}
springBoot {
	mainClass = "com.example.demo.DemoApplication"
}
`
	obs := parseGradleMainClass("build.gradle", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d: %v", len(obs), obs)
	}
	if obs[0].Name != "com.example.demo.DemoApplication" {
		t.Errorf("Name = %q; want com.example.demo.DemoApplication", obs[0].Name)
	}
	if obs[0].Properties["interface_kind"] != "gradle_main_class" {
		t.Errorf("interface_kind = %q", obs[0].Properties["interface_kind"])
	}
}

func TestParseGradleKtsMainClass(t *testing.T) {
	content := `plugins {
	id("org.springframework.boot") version "3.2.0"
}
tasks.bootJar {
	mainClass.set("com.example.Application")
}
`
	obs := parseGradleMainClass("build.gradle.kts", []byte(content))
	if len(obs) != 1 {
		t.Fatalf("want 1 observation, got %d: %v", len(obs), obs)
	}
	if obs[0].Name != "com.example.Application" {
		t.Errorf("Name = %q; want com.example.Application", obs[0].Name)
	}
}

func TestParseGradleMainClassNone(t *testing.T) {
	content := `apply plugin: 'java'
dependencies { implementation 'org.springframework.boot:spring-boot-starter' }
`
	obs := parseGradleMainClass("build.gradle", []byte(content))
	if len(obs) != 0 {
		t.Fatalf("unexpected observations: %v", obs)
	}
}
