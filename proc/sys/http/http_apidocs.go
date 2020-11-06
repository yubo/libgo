package http

import (
	"errors"
	"strings"

	restful "github.com/emicklei/go-restful"
	restfulspec "github.com/emicklei/go-restful-openapi"
	"github.com/go-openapi/spec"
	"github.com/yubo/golib/openapi"
	"github.com/yubo/golib/openapi/api"
	"github.com/yubo/goswagger"
	"k8s.io/klog/v2"
)

var (
	errExist = errors.New("object exists")
)

// installApidocs handle
// /apidocs.json
func (p *Module) installApidocs() {
	if !p.Apidocs.Enabled {
		return
	}
	wss := p.RegisteredWebServices()
	ws := restfulspec.NewOpenAPIService(restfulspec.Config{
		// you control what services are visible
		WebServices: wss,
		APIPath:     "/apidocs.json",
		PostBuildSwaggerObjectHandler: func(swo *spec.Swagger) {
			swo.Info = &spec.Info{InfoProps: p.Apidocs.InfoProps}
			swo.Tags = p.swaggerTags
			swo.SecurityDefinitions = p.securitySchemes
			enrichSwaggeerObjectSecurity(wss, swo)
		},
	})
	p.Add(ws)
}

// installSwagger handle
func (p *Module) installSwagger() {
	if p.Swagger.Enabled {
		goswagger.New(&p.Config.Swagger).Install(p)
	}
}

func (p *Module) SwaggerTagsRegister(tags ...spec.Tag) {
	p.swaggerTags = append(p.swaggerTags, tags...)
}

func (p *Module) SwaggerTagRegister(name, desc string) {
	p.swaggerTags = append(p.swaggerTags, spec.Tag{
		TagProps: spec.TagProps{
			Name:        name,
			Description: desc,
		}})
}

func (p *Module) SecuritySchemeRegister(name string, s *spec.SecurityScheme) error {
	if p.securitySchemes[name] != nil {
		return errExist
	}
	p.securitySchemes[name] = s
	return nil
}

func enrichSwaggeerObjectSecurity(wss []*restful.WebService, swo *spec.Swagger) {
	// loop through all registerd web services
	for _, ws := range wss {
		for _, route := range ws.Routes() {

			// grab route metadata for a SecurityDefinition
			secdefn, ok := route.Metadata[api.SecurityDefinitionKey]
			if !ok {
				continue
			}

			// grab pechelper.OAISecurity from the stored interface{}
			var sEntry openapi.OAISecurity
			switch v := secdefn.(type) {
			case *openapi.OAISecurity:
				sEntry = *v
			case openapi.OAISecurity:
				sEntry = v
			default:
				// not valid type
				klog.Warningf("skipping Security openapi spec for %s:%s, invalid metadata type %v", route.Method, route.Path, v)
				continue
			}

			if _, ok := swo.SecurityDefinitions[sEntry.Name]; !ok {
				// klog.Warningf("skipping Security openapi spec for %s:%s, '%s' not found in SecurityDefinitions", route.Method, route.Path, sEntry.Name)
				continue
			}

			// grab path and path item in openapi spec
			path, err := swo.Paths.JSONLookup(strings.TrimRight(route.Path, "/"))
			if err != nil {
				klog.Warningf("skipping Security openapi spec for %s:%s, %s", route.Method, route.Path, err.Error())
				path, err = swo.Paths.JSONLookup(route.Path[:len(route.Path)-1])
				if err != nil {
					klog.Warningf("skipping Security openapi spec for %s:%s, %s", route.Method, route.Path[:len(route.Path)-1], err.Error())
					continue
				}
			}
			pItem := path.(*spec.PathItem)

			// Update respective path Option based on method
			var pOption *spec.Operation
			switch method := strings.ToLower(route.Method); method {
			case "get":
				pOption = pItem.Get
			case "post":
				pOption = pItem.Post
			case "patch":
				pOption = pItem.Patch
			case "delete":
				pOption = pItem.Delete
			case "put":
				pOption = pItem.Put
			case "head":
				pOption = pItem.Head
			case "options":
				pOption = pItem.Options
			default:
				// unsupported method
				klog.Warningf("skipping Security openapi spec for %s:%s, unsupported method '%s'", route.Method, route.Path, route.Method)
				continue
			}

			// update the pOption with security entry
			pOption.SecuredWith(sEntry.Name, sEntry.Scopes...)
		}
	}

}
